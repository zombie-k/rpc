package warden

import (
	"context"
	"fmt"
	"github.com/pkg/errors"
	xtime "github.com/zombie-k/rpc/library/time"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"math"
	"net/url"
	"os"
	"sync"
	"time"
)

var (
	_defaultCliConf = &ClientConfig{
		Dial:              xtime.Duration(time.Second * 10),
		Timeout:           xtime.Duration(time.Millisecond * 250),
		KeepAliveInterval: xtime.Duration(time.Second * 60),
		KeepAliveTimeout:  xtime.Duration(time.Second * 20),
	}
	_abortIndex int8 = math.MaxInt8 / 2
)

type ClientConfig struct {
	Dial                   xtime.Duration
	Timeout                xtime.Duration
	Method                 map[string]*ClientConfig
	NonBlock               bool
	KeepAliveInterval      xtime.Duration
	KeepAliveTimeout       xtime.Duration
	KeepAliveWithoutStream bool
}

type Client struct {
	conf     *ClientConfig
	mutex    sync.RWMutex
	opts     []grpc.DialOption
	handlers []grpc.UnaryClientInterceptor
}

func NewClient(conf *ClientConfig, opt ...grpc.DialOption) *Client {
	c := new(Client)
	if err := c.SetConfig(conf); err != nil {
		panic(err)
	}
	c.UseOpt(opt...)
	return c
}

// SetConfig hot reloads client config
func (c *Client) SetConfig(conf *ClientConfig) (err error) {
	if conf == nil {
		conf = _defaultCliConf
	}
	if conf.Dial <= 0 {
		conf.Dial = xtime.Duration(time.Second * 10)
	}
	if conf.Timeout <= 0 {
		conf.Timeout = xtime.Duration(time.Millisecond * 250)
	}
	if conf.KeepAliveInterval <= 0 {
		conf.KeepAliveInterval = xtime.Duration(time.Second * 60)
	}
	if conf.KeepAliveTimeout <= 0 {
		conf.KeepAliveTimeout = xtime.Duration(time.Second * 20)
	}
	c.mutex.Lock()
	c.conf = conf
	c.mutex.Unlock()

	return nil
}

func (c *Client) Use(handlers ...grpc.UnaryClientInterceptor) *Client {
	finalSize := len(c.handlers) + len(handlers)
	if finalSize >= int(_abortIndex) {
		panic("warden: client use too many handlers")
	}
	mergedHandlers := make([]grpc.UnaryClientInterceptor, finalSize)
	copy(mergedHandlers, c.handlers)
	copy(mergedHandlers[len(c.handlers):], handlers)
	c.handlers = mergedHandlers
	return c
}

func (c *Client) UseOpt(opts ...grpc.DialOption) *Client {
	c.opts = append(c.opts, opts...)
	return c
}

func (c *Client) cloneOption() []grpc.DialOption {
	dialOptions := make([]grpc.DialOption, len(c.opts))
	copy(dialOptions, c.opts)
	return dialOptions
}

func (c *Client) Dial(ctx context.Context, target string, opts ...grpc.DialOption) (conn *grpc.ClientConn, err error) {
	opts = append(opts, grpc.WithInsecure())
	return c.dial(ctx, target, opts...)
}

func (c *Client) dial(ctx context.Context, target string, opts ...grpc.DialOption) (conn *grpc.ClientConn, err error) {
	dialOptions := c.cloneOption()
	if !c.conf.NonBlock {
		dialOptions = append(dialOptions, grpc.WithBlock())
	}
	dialOptions = append(dialOptions, grpc.WithKeepaliveParams(keepalive.ClientParameters{
		Time:                time.Duration(c.conf.KeepAliveInterval),
		Timeout:             time.Duration(c.conf.KeepAliveTimeout),
		PermitWithoutStream: !c.conf.KeepAliveWithoutStream,
	}))
	dialOptions = append(dialOptions, opts...)

	var handlers []grpc.UnaryClientInterceptor
	handlers = append(handlers, c.handlers...)
	handlers = append(handlers, c.handle())

	dialOptions = append(dialOptions, grpc.WithUnaryInterceptor(chainUnaryClient(handlers)))
	c.mutex.RLock()
	conf := c.conf
	c.mutex.RUnlock()
	if conf.Dial > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(conf.Dial))
		defer cancel()
	}

	if u, e := url.Parse(target); e == nil {
		target = u.String()
	}

	if conn, err = grpc.DialContext(ctx, target, dialOptions...); err != nil {
		fmt.Fprintf(os.Stderr, "warden client:dial %s error %v!", target, err)
	}

	err = errors.WithStack(err)
	return
}

func (c *Client) handle() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) (err error) {
		if err := invoker(ctx, method, req, reply, cc, opts...); err != nil {
			fmt.Fprintf(os.Stderr, "handle error %v", err)
		}
		return
	}
}

func chainUnaryClient(handlers []grpc.UnaryClientInterceptor) grpc.UnaryClientInterceptor {
	n := len(handlers)
	if n == 0 {
		return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
			return invoker(ctx, method, req, reply, cc, opts...)
		}
	}

	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		var (
			i            int
			chainHandler grpc.UnaryInvoker
		)
		chainHandler = func(ictx context.Context, imethod string, ireq, ireply interface{}, ic *grpc.ClientConn, iopts ...grpc.CallOption) error {
			if i == n-1 {
				return invoker(ictx, imethod, ireq, ireply, ic, iopts...)
			}
			i++
			return handlers[i](ictx, imethod, ireq, ireply, ic, chainHandler, iopts...)
		}

		return handlers[0](ctx, method, req, reply, cc, chainHandler, opts...)
	}
}
