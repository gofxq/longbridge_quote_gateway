// Command auth performs the one-time OAuth authorization on an operator's machine.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/longbridge/openapi-go/oauth"
)

func main() {
	id := os.Getenv("LONGBRIDGE_OAUTH_CLIENT_ID")
	if id == "" {
		fmt.Fprintln(os.Stderr, "LONGBRIDGE_OAUTH_CLIENT_ID is required")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := oauth.New(id).OnOpenURL(func(url string) { fmt.Println("Open this authorization URL:", url) }).Build(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "OAuth authorization failed")
		os.Exit(1)
	}
	fmt.Println("OAuth credentials saved by SDK")
}
