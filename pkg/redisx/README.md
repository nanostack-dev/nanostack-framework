# redisx

Typed shared state on Redis or Valkey: values, maps, sets, queues, counters, a mutex with fencing tokens, and pub/sub topics. One import, one entry point.

```go
rx := redisx.New(rdb, redisx.WithPrefix("echopoint"))
```

`rdb` is any `redis.UniversalClient` (`*redis.Client`, `*redis.ClusterClient`, `*redis.Ring`). The `Client` does not close it. `WithPrefix` puts every key and channel under `echopoint:`.

Every structure is a small value built from a name. Nothing touches the server until you call a method. `WithTTL` and `WithCodec` return a changed copy, so you can keep a configured structure in a field and share it between goroutines.

## Shared data structures

```go
sessions := rx.Value[Session]("session:" + id).WithTTL(time.Hour)

found, err := sessions.Get(ctx)   // (functional.Option[Session], error)
if err != nil { return err }
if found.IsAbsent() { return errUnauthenticated }

err = sessions.Set(ctx, s)
written, err := sessions.SetIfAbsent(ctx, s)   // one writer wins
previous, err := sessions.Swap(ctx, s)         // write, get the old value
next, err := sessions.Update(ctx, func(cur functional.Option[Session]) (Session, error) {
    s := cur.OrElse(Session{})
    s.Hits++
    return s, nil
})
```

| Structure | Built with | Methods |
|---|---|---|
| `Value[T]` | `rx.Value[T](name)` | `Get`, `Set`, `SetIfAbsent`, `Swap`, `Update`, `Delete`, `Exists` |
| `Map[V]` | `rx.Map[V](name)` | `Get`, `Set`, `SetAll`, `SetIfAbsent`, `Delete`, `Has`, `Len`, `All`, `Clear` |
| `Set[T]` | `rx.Set[T](name)` | `Add`, `Remove`, `Contains`, `Members`, `Len`, `Clear` |
| `Queue[T]` | `rx.Queue[T](name)` | `Push`, `Pop`, `PopWait`, `Peek`, `Len`, `Clear` |
| `Counter` | `rx.Counter(name)` | `Add`, `Incr`, `Get`, `Reset` |

- **Absence is not an error.** A missing key, field or item comes back as an absent `functional.Option`, never as an error.
- **Codecs.** `string` is stored as it is, and every other type as JSON. Pass `redisx.JSON[T]()`, `redisx.Text()` or your own `Codec[T]` to `WithCodec`.
- **TTL.** On a `Value`, `Set` without a TTL removes an earlier expiry, and `Update` without a TTL keeps it. On a `Map`, `Set`, `Queue` or `Counter`, every write moves the expiry of the whole key to TTL from now. This suits presence and short-lived windows.
- **`Update`** is optimistic: it watches the key and runs your function again when another writer changed the value in between. After 32 conflicting attempts it returns `ErrConflict`. Keep the function free of side effects.
- **`Queue`** delivers each item to exactly one `Pop` caller, but a popped item is gone. If the process stops before it handles the item, the item is lost. Durable work belongs in `modules/pgqueue`. `PopWait` waits in whole seconds, with a minimum of one second.
- `Map.All` and `Set.Members` read the whole key. Use them on small keys only.

## Mutex

```go
err := rx.Mutex("provision:" + orgID).
    WithTTL(30 * time.Second).      // lease length, refreshed while Do runs
    WithWait(15 * time.Second).     // optional cap on the wait
    Do(ctx, func(ctx context.Context) error {
        return provision(ctx)
    })
switch {
case errors.Is(err, redisx.ErrLocked):    // the wait cap was reached
case errors.Is(err, redisx.ErrLeaseLost): // the work was possibly not exclusive
}
```

| Call | Waits | Returns when busy |
|---|---|---|
| `Do(ctx, fn)` | yes | `ErrLocked` at the `WithWait` cap, or the context error |
| `TryDo(ctx, fn)` | no | `ErrLocked` |
| `Lock(ctx)` | yes | a `*Lease`, or the same errors as `Do` |
| `TryLock(ctx)` | no | a `*Lease`, or `ErrLocked` |

`Do` refreshes the lease every third of the TTL while `fn` runs, and releases it afterwards, also when `fn` panics. The lease is lost in two cases:
- the server says another holder has the mutex;
- no refresh has succeeded for a whole TTL.

In both cases `Do` cancels the context it gave `fn`, with `ErrLeaseLost` as the cause, and returns an error that matches `ErrLeaseLost`.

With `Lock` or `TryLock` you hold the `*Lease` yourself: call `Refresh`, `TTL` and `Release`. `Release` on a lease that is no longer yours returns `ErrLeaseLost` and leaves the new holder's lease alone.

**A lease can expire.** This is what lets the mutex survive a crashed holder, but a holder that stalls for longer than the TTL can lose the lease while it still runs. Every lease has a `Fence()` that is greater than the fence of every earlier lease on the same mutex. If the resource you protect can refuse writes that carry an old fence, pass it the fence.

The lease and fence keys of a mutex share a hash tag (`{echopoint:provision:org_1}:lease`), so the mutex also works on Redis Cluster. The fence key never expires: it costs one small key per mutex name.

## Pub/sub topics

```go
events := rx.Topic[OrderEvent]("orders:" + orgID)

receivers, err := events.Publish(ctx, OrderEvent{ID: id, Kind: "paid"})

sub, err := events.
    OnError(func(ctx context.Context, err error) { metrics.Inc("orders.bad_message") }).
    Subscribe(ctx, func(ctx context.Context, e OrderEvent) error {
        return notify(ctx, e)
    })
defer sub.Close()
```

- `Subscribe` returns after the server has confirmed the subscription, so every message published after that is delivered.
- The handler runs for one message at a time, in publication order.
- A message that does not decode, and an error from the handler, go to `OnError`, and the subscription continues. Without `OnError`, they are logged at error level with the context logger.
- `Close` stops the subscription and waits for the running handler to return. It is safe to call more than once. The subscription also ends when the context given to `Subscribe` ends; `Done` tells you when it stopped.
- Delivery is at most once. A subscriber gets only the messages published while it is connected. go-redis subscribes again after a lost connection, and messages published in between are lost.

## Testing

`pkg/testkit/redistest` gives tests a real server:

```go
func TestCheckout(t *testing.T) {
    t.Parallel()
    rx := redistest.New(t)   // a redisx.Client under a namespace of its own
    ...
}
```

- The server starts on first use, so a test binary that never asks for one starts nothing. One server serves the whole test binary.
- Each test gets its own namespace, deleted when the test ends, so tests can run in parallel.
- With `REDIS_TEST_ADDR` set, the tests use that server. Otherwise testcontainers starts `REDIS_TEST_IMAGE`, by default `valkey/valkey:8.1-alpine`, and removes it when the test binary exits.
- `redistest.Client(t)` and `redistest.Addr(t)` give lower-level access.

This package's own suites run with the `integration` tag:

```sh
go test -tags integration -race ./pkg/redisx ./pkg/testkit/redistest
REDIS_TEST_IMAGE=redis:7-alpine go test -tags integration -race ./pkg/redisx
```
