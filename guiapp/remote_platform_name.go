package guiapp

import goruntime "runtime"

var remotePlatformGOOS = func() string {
	return goruntime.GOOS
}
