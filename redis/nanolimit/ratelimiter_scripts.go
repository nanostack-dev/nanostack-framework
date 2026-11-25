// Copyright 2026 Nanostack.
//
// The Lua scripts of Redisson's RedissonRateLimiter, Copyright (c) 2013-2026
// Nikita Koksharov, licensed under the Apache License, Version 2.0
// (http://www.apache.org/licenses/LICENSE-2.0), unchanged but for layout.

package nanolimit

import "github.com/redis/go-redis/v9"

//nolint:gochecknoglobals // A script is compiled once and cached by its SHA.
var trySetRateScript = redis.NewScript(`
redis.call('hsetnx', KEYS[1], 'rate', ARGV[1]);
redis.call('hsetnx', KEYS[1], 'interval', ARGV[2]);
redis.call('hsetnx', KEYS[1], 'keepAliveTime', ARGV[4]);
local res = redis.call('hsetnx', KEYS[1], 'type', ARGV[3]);
if res == 1 and tonumber(ARGV[4]) > 0 then
  redis.call('pexpire', KEYS[1], ARGV[4]);
end;
return res;`)

//nolint:gochecknoglobals // A script is compiled once and cached by its SHA.
var setOrUpdateRateScript = redis.NewScript(`
if ARGV[7] == '1' and redis.call('exists', KEYS[1]) == 0 then
  return 0;
end;
local valueName = KEYS[2];
local permitsName = KEYS[4];
if ARGV[3] == '1' then
  valueName = KEYS[3];
  permitsName = KEYS[5];
end
local oldType = redis.call('hget', KEYS[1], 'type');
redis.call('hset', KEYS[1], 'rate', ARGV[1]);
redis.call('hset', KEYS[1], 'interval', ARGV[2]);
redis.call('hset', KEYS[1], 'type', ARGV[3]);
redis.call('hset', KEYS[1], 'keepAliveTime', ARGV[4]);
if tonumber(ARGV[4]) > 0 then
  redis.call('pexpire', KEYS[1], ARGV[4]);
end;
if oldType ~= false and oldType ~= ARGV[3] then
  redis.call('del', KEYS[2], KEYS[3], KEYS[4], KEYS[5]);
  return 1;
end;
if ARGV[6] == '0' then
  redis.call('del', valueName, permitsName);
  return 1;
end;
local rate = tonumber(ARGV[1]);
local interval = tonumber(ARGV[2]);
local now = tonumber(ARGV[5]);
local currentValue = redis.call('get', valueName);
if currentValue == false then
  currentValue = rate;
else
  currentValue = tonumber(currentValue);
end;
redis.call('zremrangebyscore', permitsName, 0, now - interval);
local values = redis.call('zrange', permitsName, 0, -1);
local used = 0;
for i, v in ipairs(values) do
  local random, permits = struct.unpack('Bc0I', v);
  used = used + permits;
end;
local newValue = rate - used;
if newValue < 0 then
  newValue = 0;
end;
redis.call('set', valueName, newValue);
local keepAliveTime = redis.call('hget', KEYS[1], 'keepAliveTime');
if (keepAliveTime ~= false and tonumber(keepAliveTime) > 0) then
  redis.call('pexpire', KEYS[1], keepAliveTime);
  redis.call('pexpire', valueName, keepAliveTime);
  redis.call('pexpire', permitsName, keepAliveTime);
else
  local ttl = redis.call('pttl', KEYS[1]);
  if ttl > 0 then
    redis.call('pexpire', valueName, ttl);
    redis.call('pexpire', permitsName, ttl);
  end;
end;
return 1;`)

// tryAcquireScript returns nil when it took the permits, and otherwise the
// milliseconds until the oldest permit comes back.
//
//nolint:gochecknoglobals // A script is compiled once and cached by its SHA.
var tryAcquireScript = redis.NewScript(`
local rate = redis.call('hget', KEYS[1], 'rate');
local interval = redis.call('hget', KEYS[1], 'interval');
local type = redis.call('hget', KEYS[1], 'type');
assert(rate ~= false and interval ~= false and type ~= false, 'RateLimiter is not initialized')
local valueName = KEYS[2];
local permitsName = KEYS[4];
if type == '1' then
  valueName = KEYS[3];
  permitsName = KEYS[5];
end;
assert(tonumber(rate) >= tonumber(ARGV[1]), 'Requested permits amount cannot exceed defined rate');
local currentValue = redis.call('get', valueName);
local res;
if currentValue ~= false then
  local expiredValues = redis.call('zrangebyscore', permitsName, 0, tonumber(ARGV[2]) - interval);
  local released = 0;
  for i, v in ipairs(expiredValues) do
    local random, permits = struct.unpack('Bc0I', v);
    released = released + permits;
  end;
  if released > 0 then
    redis.call('zremrangebyscore', permitsName, 0, tonumber(ARGV[2]) - interval);
    if tonumber(currentValue) + released > tonumber(rate) then
      local values = redis.call('zrange', permitsName, 0, -1);
      local used = 0;
      for i, v in ipairs(values) do
        local random, permits = struct.unpack('Bc0I', v);
        used = used + permits;
      end;
      currentValue = tonumber(rate) - used;
    else
      currentValue = tonumber(currentValue) + released;
    end;
    redis.call('set', valueName, currentValue);
  end;
  if tonumber(currentValue) < tonumber(ARGV[1]) then
    local firstValue = redis.call('zrange', permitsName, 0, 0, 'withscores');
    res = 3 + interval - (tonumber(ARGV[2]) - tonumber(firstValue[2]));
  else
    redis.call('zadd', permitsName, ARGV[2], struct.pack('Bc0I', string.len(ARGV[3]), ARGV[3], ARGV[1]));
    redis.call('decrby', valueName, ARGV[1]);
    res = nil;
  end;
else
  redis.call('set', valueName, rate);
  redis.call('zadd', permitsName, ARGV[2], struct.pack('Bc0I', string.len(ARGV[3]), ARGV[3], ARGV[1]));
  redis.call('decrby', valueName, ARGV[1]);
  res = nil;
end;
local keepAliveTime = redis.call('hget', KEYS[1], 'keepAliveTime');
if (keepAliveTime ~= false and tonumber(keepAliveTime) > 0) then
  redis.call('pexpire', KEYS[1], keepAliveTime);
  redis.call('pexpire', valueName, keepAliveTime);
  redis.call('pexpire', permitsName, keepAliveTime);
else
  local ttl = redis.call('pttl', KEYS[1]);
  if ttl > 0 then
    redis.call('pexpire', valueName, ttl);
    redis.call('pexpire', permitsName, ttl);
  end;
end;
return res;`)

//nolint:gochecknoglobals // A script is compiled once and cached by its SHA.
var releaseRateScript = redis.NewScript(`
local rate = redis.call('hget', KEYS[1], 'rate');
local interval = redis.call('hget', KEYS[1], 'interval');
local type = redis.call('hget', KEYS[1], 'type');
assert(rate ~= false and interval ~= false and type ~= false, 'RateLimiter is not initialized');
local valueName = KEYS[2];
local permitsName = KEYS[4];
if type == '1' then
  valueName = KEYS[3];
  permitsName = KEYS[5];
end;
local toRelease = tonumber(ARGV[1]);
local values = redis.call('zrange', permitsName, 0, -1, 'withscores');
for i = 1, #values - 1, 2 do
  if toRelease <= 0 then break; end;
  local v = values[i];
  local score = values[i + 1];
  local random, permits = struct.unpack('Bc0I', v);
  redis.call('zrem', permitsName, v);
  if permits <= toRelease then
    toRelease = toRelease - permits;
  else
    local remained = permits - toRelease;
    redis.call('zadd', permitsName, score, struct.pack('Bc0I', string.len(random), random, remained));
    toRelease = 0;
  end;
end;
local currentValue = redis.call('get', valueName);
if currentValue == false then
  currentValue = tonumber(rate);
else
  currentValue = tonumber(currentValue);
end;
local newValue = currentValue + tonumber(ARGV[1]);
if newValue > tonumber(rate) then
  newValue = tonumber(rate);
end;
redis.call('set', valueName, newValue);
local keepAliveTime = redis.call('hget', KEYS[1], 'keepAliveTime');
if (keepAliveTime ~= false and tonumber(keepAliveTime) > 0) then
  redis.call('pexpire', KEYS[1], keepAliveTime);
  redis.call('pexpire', valueName, keepAliveTime);
  redis.call('pexpire', permitsName, keepAliveTime);
else
  local ttl = redis.call('pttl', KEYS[1]);
  if ttl > 0 then
    redis.call('pexpire', valueName, ttl);
    redis.call('pexpire', permitsName, ttl);
  end;
end;`)

//nolint:gochecknoglobals // A script is compiled once and cached by its SHA.
var availableRatePermitsScript = redis.NewScript(`
local rate = redis.call('hget', KEYS[1], 'rate');
local interval = redis.call('hget', KEYS[1], 'interval');
local type = redis.call('hget', KEYS[1], 'type');
assert(rate ~= false and interval ~= false and type ~= false, 'RateLimiter is not initialized')
local valueName = KEYS[2];
local permitsName = KEYS[4];
if type == '1' then
  valueName = KEYS[3];
  permitsName = KEYS[5];
end;
local currentValue = redis.call('get', valueName);
if currentValue == false then
  redis.call('set', valueName, rate);
  return rate;
else
  local expiredValues = redis.call('zrangebyscore', permitsName, 0, tonumber(ARGV[1]) - interval);
  local released = 0;
  for i, v in ipairs(expiredValues) do
    local random, permits = struct.unpack('Bc0I', v);
    released = released + permits;
  end;
  if released > 0 then
    redis.call('zremrangebyscore', permitsName, 0, tonumber(ARGV[1]) - interval);
    currentValue = tonumber(currentValue) + released;
    redis.call('set', valueName, currentValue);
  end;
  return currentValue;
end;`)
