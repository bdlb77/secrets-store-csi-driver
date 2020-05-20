package main

import (
	"context"
	"time"

	reconciler "sigs.k8s.io/secrets-store-csi-driver/pkg/reconciler"
)

func main() {
	var ctx context.Context
	// interval every X (time.Second)
	pollInterval := 60

	timerCh := time.Tick(time.Duration(pollInterval) * time.Second)

	for range timerCh {
		reconciler.Reconciler(ctx)
	}

}
