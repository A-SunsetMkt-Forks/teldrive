package tgc

import (
	"context"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/divyam234/teldrive/internal/config"
	"github.com/divyam234/teldrive/internal/kv"
	"github.com/divyam234/teldrive/internal/recovery"
	"github.com/divyam234/teldrive/internal/retry"
	"github.com/divyam234/teldrive/internal/utils"
	"github.com/gotd/contrib/middleware/floodwait"
	"github.com/gotd/contrib/middleware/ratelimit"
	tdclock "github.com/gotd/td/clock"
	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/dcs"
	"github.com/pkg/errors"
	"golang.org/x/net/proxy"
	"golang.org/x/time/rate"
)

func defaultMiddlewares(ctx context.Context, retries int) ([]telegram.Middleware, error) {

	return []telegram.Middleware{
		recovery.New(ctx, Backoff(tdclock.System)),
		retry.New(retries),
		floodwait.NewSimpleWaiter(),
	}, nil
}

func New(ctx context.Context, config *config.TGConfig, handler telegram.UpdateHandler, storage session.Storage, middlewares ...telegram.Middleware) (*telegram.Client, error) {

	var dialer dcs.DialFunc = proxy.Direct.DialContext
	if config.Proxy != "" {
		d, err := utils.Proxy.GetDial(config.Proxy)
		if err != nil {
			return nil, errors.Wrap(err, "get dialer")
		}
		dialer = d.DialContext
	}

	opts := telegram.Options{
		Resolver: dcs.Plain(dcs.PlainOptions{
			Dial: dialer,
		}),
		Device: telegram.DeviceConfig{
			DeviceModel:    config.DeviceModel,
			SystemVersion:  config.SystemVersion,
			AppVersion:     config.AppVersion,
			SystemLangCode: config.SystemLangCode,
			LangPack:       config.LangPack,
			LangCode:       config.LangCode,
		},
		SessionStorage: storage,
		RetryInterval:  time.Second,
		MaxRetries:     10,
		DialTimeout:    10 * time.Second,
		Middlewares:    middlewares,
		UpdateHandler:  handler,
	}

	return telegram.NewClient(config.AppId, config.AppHash, opts), nil
}

func NoAuthClient(ctx context.Context, config *config.TGConfig, handler telegram.UpdateHandler, storage session.Storage) (*telegram.Client, error) {
	middlewares, _ := defaultMiddlewares(ctx, 5)
	middlewares = append(middlewares, ratelimit.New(rate.Every(time.Millisecond*100), 5))
	return New(ctx, config, handler, storage, middlewares...)
}

func AuthClient(ctx context.Context, config *config.TGConfig, sessionStr string) (*telegram.Client, error) {
	data, err := session.TelethonSession(sessionStr)

	if err != nil {
		return nil, err
	}

	var (
		storage = new(session.StorageMemory)
		loader  = session.Loader{Storage: storage}
	)

	if err := loader.Save(context.TODO(), data); err != nil {
		return nil, err
	}
	middlewares, _ := defaultMiddlewares(ctx, 5)
	middlewares = append(middlewares, ratelimit.New(rate.Every(time.Millisecond*
		time.Duration(config.Rate)), config.RateBurst))
	return New(ctx, config, nil, storage, middlewares...)
}

func BotClient(ctx context.Context, KV kv.KV, config *config.TGConfig, token string, retries int) (*telegram.Client, error) {
	storage := kv.NewSession(KV, kv.Key("botsession", token))
	middlewares, _ := defaultMiddlewares(ctx, retries)
	if config.RateLimit {
		middlewares = append(middlewares, ratelimit.New(rate.Every(time.Millisecond*
			time.Duration(config.Rate)), config.RateBurst))

	}
	return New(ctx, config, nil, storage, middlewares...)
}
func Backoff(_clock tdclock.Clock) backoff.BackOff {
	b := backoff.NewExponentialBackOff()

	b.Multiplier = 1.1
	b.MaxElapsedTime = time.Duration(120) * time.Second
	b.Clock = _clock
	return b
}
