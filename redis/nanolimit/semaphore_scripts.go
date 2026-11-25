// Copyright 2026 Nanostack.
//
// The Lua scripts of Redisson's RedissonPermitExpirableSemaphore, Copyright (c) 2013-2026
// Nikita Koksharov, licensed under the Apache License, Version 2.0
// (http://www.apache.org/licenses/LICENSE-2.0), unchanged but for layout.

package nanolimit

import "github.com/redis/go-redis/v9"

// tryAcquireSemaphoreScript first reclaims the expired permits, then takes
// the permits. It returns 'OK', ':<score>' (the earliest lease end, when a
// permit with a lease is held) or nil.
//
//nolint:gochecknoglobals // A script is compiled once and cached by its SHA.
var tryAcquireSemaphoreScript = redis.NewScript(`
local expiredIds = redis.call('zrangebyscore', KEYS[2], 0, ARGV[3], 'limit', 0, ARGV[1]);
if #expiredIds > 0 then
  redis.call('zrem', KEYS[2], unpack(expiredIds));
  local value = redis.call('incrby', KEYS[1], #expiredIds);
  if tonumber(value) > 0 then
    redis.call(ARGV[5], KEYS[3], value);
  end;
end;
local value = redis.call('get', KEYS[1]);
if (value ~= false and tonumber(value) >= tonumber(ARGV[1])) then
  redis.call('decrby', KEYS[1], ARGV[1]);
  for i = 6, #ARGV, 1 do
    redis.call('zadd', KEYS[2], ARGV[2], ARGV[i]);
  end;
  local ttl = redis.call('pttl', KEYS[1]);
  if ttl > 0 then
    redis.call('pexpire', KEYS[2], ttl);
  end;
  return 'OK';
end;
local v = redis.call('zrange', KEYS[2], 0, 0, 'WITHSCORES');
if v[1] ~= nil and v[2] ~= ARGV[4] then
  return ':' .. tostring(v[2]);
end
return nil;`)

//nolint:gochecknoglobals // A script is compiled once and cached by its SHA.
var tryReleaseSemaphoreScript = redis.NewScript(`
local expire = redis.call('zscore', KEYS[3], ARGV[1]);
local removed = redis.call('zrem', KEYS[3], ARGV[1]);
if tonumber(removed) ~= 1 then
  return 0;
end;
local value = redis.call('incrby', KEYS[1], ARGV[2]);
redis.call(ARGV[4], KEYS[2], value);
if tonumber(expire) <= tonumber(ARGV[3]) then
  return 0;
end;
return 1;`)

//nolint:gochecknoglobals // A script is compiled once and cached by its SHA.
var tryReleaseManySemaphoreScript = redis.NewScript(`
for i = 4, #ARGV, 1 do
  local expire = redis.call('zscore', KEYS[3], ARGV[i]);
  if expire == false or tonumber(expire) <= tonumber(ARGV[2]) then
    return 0;
  end;
end;
local expiredIds = redis.call('zrangebyscore', KEYS[3], 0, ARGV[2], 'limit', 0, -1);
if #expiredIds > 0 then
  redis.call('zrem', KEYS[3], unpack(expiredIds));
  local value = redis.call('incrby', KEYS[1], #expiredIds);
  if tonumber(value) > 0 then
    redis.call(ARGV[3], KEYS[2], value);
  end;
end;
local keys = {};
for i = 4, #ARGV, 1 do
  table.insert(keys, ARGV[i]);
end;
local removed = redis.call('zrem', KEYS[3], unpack(keys));
redis.call('incrby', KEYS[1], removed);
redis.call(ARGV[3], KEYS[2], removed);
return removed;`)

//nolint:gochecknoglobals // A script is compiled once and cached by its SHA.
var availableSemaphorePermitsScript = redis.NewScript(`
local expiredIds = redis.call('zrangebyscore', KEYS[2], 0, ARGV[1], 'limit', 0, -1);
if #expiredIds > 0 then
  redis.call('zrem', KEYS[2], unpack(expiredIds));
  local value = redis.call('incrby', KEYS[1], #expiredIds);
  if tonumber(value) > 0 then
    redis.call(ARGV[2], KEYS[3], value);
  end;
  return value;
end;
local ret = redis.call('get', KEYS[1]);
return ret == false and 0 or ret;`)

//nolint:gochecknoglobals // A script is compiled once and cached by its SHA.
var semaphorePermitsScript = redis.NewScript(`
local expiredIds = redis.call('zrangebyscore', KEYS[2], 0, ARGV[1], 'limit', 0, -1);
if #expiredIds > 0 then
  redis.call('zrem', KEYS[2], unpack(expiredIds));
  local value = redis.call('incrby', KEYS[1], #expiredIds);
  if tonumber(value) > 0 then
    redis.call(ARGV[2], KEYS[3], value);
  end;
end;
local available = redis.call('get', KEYS[1]);
if available == false then
  return 0
end;
local acquired = redis.call('zcount', KEYS[2], 0, '+inf');
if acquired == false then
  return tonumber(available)
end;
return tonumber(available) + acquired;`)

//nolint:gochecknoglobals // A script is compiled once and cached by its SHA.
var acquiredSemaphorePermitsScript = redis.NewScript(`
local expiredIds = redis.call('zrangebyscore', KEYS[2], 0, ARGV[1], 'limit', 0, -1);
if #expiredIds > 0 then
  redis.call('zrem', KEYS[2], unpack(expiredIds));
  local value = redis.call('incrby', KEYS[1], #expiredIds);
  if tonumber(value) > 0 then
    redis.call(ARGV[2], KEYS[3], value);
  end;
end;
local acquired = redis.call('zcount', KEYS[2], 0, '+inf');
return acquired == false and 0 or acquired;`)

//nolint:gochecknoglobals // A script is compiled once and cached by its SHA.
var trySetSemaphorePermitsScript = redis.NewScript(`
local value = redis.call('get', KEYS[1]);
if (value == false) then
  redis.call('set', KEYS[1], ARGV[1]);
  redis.call(ARGV[2], KEYS[2], ARGV[1]);
  return 1;
end;
return 0;`)

//nolint:gochecknoglobals // A script is compiled once and cached by its SHA.
var setSemaphorePermitsScript = redis.NewScript(`
local available = redis.call('get', KEYS[1]);
if (available == false) then
  redis.call('set', KEYS[1], ARGV[1]);
  redis.call(ARGV[2], KEYS[2], ARGV[1]);
  return;
end;
local acquired = redis.call('zcount', KEYS[3], 0, '+inf');
local maximum = (acquired == false and 0 or acquired) + tonumber(available);
if (maximum == tonumber(ARGV[1])) then
  return;
end;
redis.call('incrby', KEYS[1], tonumber(ARGV[1]) - maximum);
redis.call(ARGV[2], KEYS[2], ARGV[1]);`)

//nolint:gochecknoglobals // A script is compiled once and cached by its SHA.
var addSemaphorePermitsScript = redis.NewScript(`
local value = redis.call('get', KEYS[1]);
if (value == false) then
  value = 0;
end;
redis.call('set', KEYS[1], tonumber(value) + tonumber(ARGV[1]));
if tonumber(ARGV[1]) > 0 then
  redis.call(ARGV[2], KEYS[2], ARGV[1]);
end;`)

//nolint:gochecknoglobals // A script is compiled once and cached by its SHA.
var updateLeaseTimeScript = redis.NewScript(`
local expiredIds = redis.call('zrangebyscore', KEYS[2], 0, ARGV[3], 'limit', 0, -1);
if #expiredIds > 0 then
  redis.call('zrem', KEYS[2], unpack(expiredIds));
  local value = redis.call('incrby', KEYS[1], #expiredIds);
  if tonumber(value) > 0 then
    redis.call(ARGV[4], KEYS[3], value);
  end;
end;
local value = redis.call('zscore', KEYS[2], ARGV[1]);
if (value ~= false) then
  redis.call('zadd', KEYS[2], ARGV[2], ARGV[1]);
  return 1;
end;
return 0;`)

//nolint:gochecknoglobals // A script is compiled once and cached by its SHA.
var leaseTimeScript = redis.NewScript(`
local expiredIds = redis.call('zrangebyscore', KEYS[2], 0, ARGV[3], 'limit', 0, -1);
if #expiredIds > 0 then
  redis.call('zrem', KEYS[2], unpack(expiredIds));
  local value = redis.call('incrby', KEYS[1], #expiredIds);
  if tonumber(value) > 0 then
    redis.call(ARGV[4], KEYS[3], value);
  end;
end;
local value = redis.call('zscore', KEYS[2], ARGV[1]);
if (value ~= false) then
  return tonumber(value) == tonumber(ARGV[2]) and -1 or tonumber(value) - tonumber(ARGV[3]);
end;
return 0;`)
