package desktop

import "github.com/ebitengine/purego"

// SetApplicationIdentity sets the GTK program name before MyGo starts.
// GTK uses it for the X11 class and the Wayland application ID.
func SetApplicationIdentity(id string) error {
	if id == "" {
		return nil
	}
	lib, err := purego.Dlopen("libglib-2.0.so.0", purego.RTLD_LAZY|purego.RTLD_LOCAL)
	if err != nil {
		return err
	}
	var setName func(string)
	purego.RegisterLibFunc(&setName, lib, "g_set_prgname")
	setName(id)
	return nil
}
