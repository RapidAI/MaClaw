package mac

//type ActivationPolicy int
//
//const (
//	NSApplicationActivationPolicyRegular    ActivationPolicy = 0
//	NSApplicationActivationPolicyAccessory  ActivationPolicy = 1
//	NSApplicationActivationPolicyProhibited ActivationPolicy = 2
//)

type AboutInfo struct {
	Title   string
	Message string
	Icon    []byte
}

// Options are options specific to Mac
type Options struct {
	TitleBar             *TitleBar
	Appearance           AppearanceType
	ContentProtection    bool
	WebviewIsTransparent bool
	WindowIsTranslucent  bool
	Preferences          *Preferences
	DisableZoom          bool
	// ActivationPolicy     ActivationPolicy
	About      *AboutInfo
	OnFileOpen func(filePath string) `json:"-"`
	// OnLaunchFileBatch runs once, after applicationDidFinishLaunching releases
	// the wait in startFileOpenProcessor. paths is every file already sitting
	// in openFilepathBuffer, or an empty slice when the launch had no document.
	// The callback must not be inferred from an empty buffer before that signal.
	OnLaunchFileBatch func(paths []string) `json:"-"`
	OnUrlOpen         func(filePath string) `json:"-"`
	// URLHandlers          map[string]func(string)
}
