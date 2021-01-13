package warden

import (
	"context"
	"fmt"
	xtime "github.com/zombie-k/rpc/library/time"
	"google.golang.org/grpc"
	"testing"
	"time"
)

func TestChainUnaryClient(t *testing.T) {
	var orders []string
	factory := func(name string) grpc.UnaryClientInterceptor {
		return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
			orders = append(orders, name+"-in")
			err := invoker(ctx, method, req, reply, cc, opts...)
			orders = append(orders, name+"-out")
			return err
		}
	}
	fmt.Println(orders)
	handlers := []grpc.UnaryClientInterceptor{factory("h1"), factory("h2"), factory("h3")}
	fmt.Println(orders)
	interceptor := chainUnaryClient(handlers)
	interceptor(context.Background(), "test", nil, nil, nil, func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		orders = append(orders, "test")
		return nil
	})
	fmt.Println(orders)
}

func TestNewClient(t *testing.T) {
	client := NewClient(&ClientConfig{
		Dial:     xtime.Duration(time.Second * 10),
		Timeout:  xtime.Duration(time.Second * 10),
		NonBlock: false,
	})

	client.Use(func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) (err error) {
		fmt.Println("middleware in")
		err = invoker(ctx, method, req, reply, cc, opts...)
		fmt.Println("middleware out")
		return
	})

	conn, err := client.Dial(context.Background(), "")
	if err != nil {
		fmt.Println("connect err", err)
		return
	}
	defer conn.Close()
	c := NewSearchServiceClient(conn)
	res, err := c.SearchSys(context.Background(), &SearchReq{
		Cmd: "pwd",
	})
	fmt.Println(res, res.Item)
}
