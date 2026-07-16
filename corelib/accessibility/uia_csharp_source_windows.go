//go:build windows

package accessibility

import _ "embed"

//go:embed tools/MaclawUIASidecar/Program.cs
var uiaCSharpSource string
