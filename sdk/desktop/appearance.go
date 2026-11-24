package desktop

import "github.com/egoist/mygo"

// ApplyAppearance makes the windows light, dark, or follow the system.
func ApplyAppearance(appearance string) {
	switch appearance {
	case "light":
		mygo.Theme.SetSource(mygo.ThemeLight)
	case "dark":
		mygo.Theme.SetSource(mygo.ThemeDark)
	default:
		mygo.Theme.SetSource(mygo.ThemeSystem)
	}
}
