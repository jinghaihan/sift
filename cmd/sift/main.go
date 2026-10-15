// Sift reviews complete feature branches in a native desktop interface.
package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/jinghaihan/sift/internal/app"
	"github.com/jinghaihan/sift/internal/cli"
)

func main() {
	if cli.Handles(os.Args[1:]) {
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
		code := cli.Run(ctx, os.Args[1:], os.Stdin, os.Stdout)
		cancel()
		os.Exit(code)
	}
	app.Run()
}
