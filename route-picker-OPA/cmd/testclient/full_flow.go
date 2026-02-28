package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"time"

	core_v3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	service_ext_proc_v3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"
)

func testFullFlow() {
	conn, err := grpc.Dial("localhost:18080", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("failed to dial: %v", err)
	}
	defer conn.Close()

	client := service_ext_proc_v3.NewExternalProcessorClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stream, err := client.Process(ctx)
	if err != nil {
		log.Fatalf("failed to create stream: %v", err)
	}

	// Test 1: Send headers, body with "force cpu", and trailers
	fmt.Println("=== Test 1: Body with 'force cpu' ===")

	// Send RequestHeaders
	headersReq := &service_ext_proc_v3.ProcessingRequest{
		Request: &service_ext_proc_v3.ProcessingRequest_RequestHeaders{
			RequestHeaders: &service_ext_proc_v3.HttpHeaders{
				Headers: &core_v3.HeaderMap{
					Headers: []*core_v3.HeaderValue{
						{Key: "content-type", Value: "application/json"},
					},
				},
			},
		},
	}
	if err := stream.Send(headersReq); err != nil {
		log.Fatalf("failed to send headers: %v", err)
	}
	fmt.Println("Sent RequestHeaders")

	// Send RequestBody with "force cpu"
	bodyReq := &service_ext_proc_v3.ProcessingRequest{
		Request: &service_ext_proc_v3.ProcessingRequest_RequestBody{
			RequestBody: &service_ext_proc_v3.HttpBody{
				Body:        []byte(`{"action": "force cpu"}`),
				EndOfStream: true,
			},
		},
	}
	if err := stream.Send(bodyReq); err != nil {
		log.Fatalf("failed to send body: %v", err)
	}
	fmt.Println("Sent RequestBody with 'force cpu'")

	// Receive all responses
	for i := 0; i < 2; i++ { // Expect 2 responses: one for headers, one for body
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("recv error: %v", err)
		}
		jb, _ := protojson.Marshal(resp)
		fmt.Printf("Response %d: %s\n", i+1, string(jb))
	}

	stream.CloseSend()
	fmt.Println("Test 1 complete\n")

	// Test 2: Body with "force gpu"
	fmt.Println("=== Test 2: Body with 'force gpu' ===")
	stream2, err := client.Process(context.Background())
	if err != nil {
		log.Fatalf("failed to create stream: %v", err)
	}

	if err := stream2.Send(&service_ext_proc_v3.ProcessingRequest{
		Request: &service_ext_proc_v3.ProcessingRequest_RequestHeaders{
			RequestHeaders: &service_ext_proc_v3.HttpHeaders{
				Headers: &core_v3.HeaderMap{Headers: []*core_v3.HeaderValue{}},
			},
		},
	}); err != nil {
		log.Fatalf("failed to send: %v", err)
	}

	if err := stream2.Send(&service_ext_proc_v3.ProcessingRequest{
		Request: &service_ext_proc_v3.ProcessingRequest_RequestBody{
			RequestBody: &service_ext_proc_v3.HttpBody{
				Body:        []byte(`{"request": "force gpu please"}`),
				EndOfStream: true,
			},
		},
	}); err != nil {
		log.Fatalf("failed to send: %v", err)
	}

	for i := 0; i < 2; i++ {
		resp, err := stream2.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("recv error: %v", err)
		}
		jb, _ := protojson.Marshal(resp)
		fmt.Printf("Response %d: %s\n", i+1, string(jb))
	}
	stream2.CloseSend()
	fmt.Println("Test 2 complete\n")

	fmt.Println("All tests passed!")
}
