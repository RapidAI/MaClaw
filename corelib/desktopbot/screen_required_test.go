package desktopbot

import (
	"errors"
	"testing"
)

func TestScreenRequiredIsATypedRetry(t *testing.T) {
	err := &ScreenRequiredError{Output: "Error: something failed"}
	if !errors.Is(err, ErrScreenRequired) || err.Code() != CodeScreenRequired {
		t.Fatalf("err=%v code=%s", err, err.Code())
	}
	if err.Error() == "Error: something failed" {
		t.Fatal("the retry sentence was dropped")
	}
}
