package main

import (
	"testing"
	"time"
)

func TestDesktopStepTimeoutPublishOutlastsPipeline(t *testing.T) {
	if got := desktopStepTimeout("publish"); got < 240*time.Second {
		t.Fatalf("publish hard timeout %s is shorter than the pipeline's own step budgets", got)
	}
	if got := desktopStepTimeout("navigate"); got != 45*time.Second {
		t.Fatalf("navigate %s", got)
	}
}
