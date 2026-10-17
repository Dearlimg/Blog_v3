package message

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

const createCooldown = time.Minute

var createRateLimitScript = redis.NewScript(`
local created = redis.call("SET", KEYS[1], "1", "NX", "PX", ARGV[1])
if created then
	return 0
end

local retryAfter = redis.call("PTTL", KEYS[1])
if retryAfter < 1 then
	return 1
end
return retryAfter
`)

type CreateRateLimiter struct {
	rdb *redis.Client
}

func NewCreateRateLimiter(rdb *redis.Client) *CreateRateLimiter {
	return &CreateRateLimiter{rdb: rdb}
}

// RetryAfter returns zero when the IP can submit now, or the remaining cooldown.
func (l *CreateRateLimiter) RetryAfter(ctx context.Context, ip string) (time.Duration, error) {
	if l == nil || l.rdb == nil {
		return 0, errors.New("message rate limiter is unavailable")
	}

	key := "message:create:rate:" + ip
	retryAfterMS, err := createRateLimitScript.Run(
		ctx,
		l.rdb,
		[]string{key},
		createCooldown.Milliseconds(),
	).Int64()
	if err != nil {
		return 0, err
	}
	if retryAfterMS <= 0 {
		return 0, nil
	}
	return time.Duration(retryAfterMS) * time.Millisecond, nil
}
