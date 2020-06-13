package main

import (
	"context"
	"time"

	"sigs.k8s.io/secrets-store-csi-driver/pkg/rotation_reconciler"
)

func main() {
	var ctx context.Context
	// interval every X (time.Second)
	pollInterval := 60

	timerCh := time.Tick(time.Duration(pollInterval) * time.Second)

	for range timerCh {
		rotation_reconciler.Reconciler(ctx)
	}

}
