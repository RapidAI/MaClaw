package main

import goruntime "runtime"

var remotePlatformGOOS = func() string {
	return goruntime.GOOS
}
