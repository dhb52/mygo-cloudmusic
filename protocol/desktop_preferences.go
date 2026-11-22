package protocol

type DesktopPreferenceFields uint16

const (
	PrefAppearance DesktopPreferenceFields = 1 << iota
	PrefAppIcon
	PrefCacheMB
	PrefNotch
	PrefNotchActivity
	PrefNotchVirtual
	PrefSmoke
	PrefFog
	PrefUseMPV
	PrefMPVExclusive
	PrefMPVGapless
	PrefMPVAudioDevice
	PrefDownloadDir

	allDesktopPreferences = (PrefDownloadDir << 1) - 1
)

const HostPatchDesktopPreferences = "preferences-patch"

// DesktopPreferencePatch selects updated fields, including explicit zero values.
type DesktopPreferencePatch struct {
	Fields DesktopPreferenceFields
	Values DesktopPreferences
}

func (p DesktopPreferencePatch) Valid() bool {
	return p.Fields&^allDesktopPreferences == 0
}

func (base DesktopPreferences) Changes(next DesktopPreferences) DesktopPreferencePatch {
	patch := DesktopPreferencePatch{Values: next}
	if base.Appearance != next.Appearance {
		patch.Fields |= PrefAppearance
	}
	if base.AppIcon != next.AppIcon {
		patch.Fields |= PrefAppIcon
	}
	if base.CacheMB != next.CacheMB {
		patch.Fields |= PrefCacheMB
	}
	if base.Notch != next.Notch {
		patch.Fields |= PrefNotch
	}
	if base.NotchActivity != next.NotchActivity {
		patch.Fields |= PrefNotchActivity
	}
	if base.NotchVirtual != next.NotchVirtual {
		patch.Fields |= PrefNotchVirtual
	}
	if base.Smoke != next.Smoke {
		patch.Fields |= PrefSmoke
	}
	if base.Fog != next.Fog {
		patch.Fields |= PrefFog
	}
	if base.UseMPV != next.UseMPV {
		patch.Fields |= PrefUseMPV
	}
	if base.MPVExclusive != next.MPVExclusive {
		patch.Fields |= PrefMPVExclusive
	}
	if base.MPVGapless != next.MPVGapless {
		patch.Fields |= PrefMPVGapless
	}
	if base.MPVAudioDevice != next.MPVAudioDevice {
		patch.Fields |= PrefMPVAudioDevice
	}
	if base.DownloadDir != next.DownloadDir {
		patch.Fields |= PrefDownloadDir
	}
	return patch
}

func (p DesktopPreferencePatch) ApplyTo(base DesktopPreferences) DesktopPreferences {
	if p.Fields&PrefAppearance != 0 {
		base.Appearance = p.Values.Appearance
	}
	if p.Fields&PrefAppIcon != 0 {
		base.AppIcon = p.Values.AppIcon
	}
	if p.Fields&PrefCacheMB != 0 {
		base.CacheMB = p.Values.CacheMB
	}
	if p.Fields&PrefNotch != 0 {
		base.Notch = p.Values.Notch
	}
	if p.Fields&PrefNotchActivity != 0 {
		base.NotchActivity = p.Values.NotchActivity
	}
	if p.Fields&PrefNotchVirtual != 0 {
		base.NotchVirtual = p.Values.NotchVirtual
	}
	if p.Fields&PrefSmoke != 0 {
		base.Smoke = p.Values.Smoke
	}
	if p.Fields&PrefFog != 0 {
		base.Fog = p.Values.Fog
	}
	if p.Fields&PrefUseMPV != 0 {
		base.UseMPV = p.Values.UseMPV
	}
	if p.Fields&PrefMPVExclusive != 0 {
		base.MPVExclusive = p.Values.MPVExclusive
	}
	if p.Fields&PrefMPVGapless != 0 {
		base.MPVGapless = p.Values.MPVGapless
	}
	if p.Fields&PrefMPVAudioDevice != 0 {
		base.MPVAudioDevice = p.Values.MPVAudioDevice
	}
	if p.Fields&PrefDownloadDir != 0 {
		base.DownloadDir = p.Values.DownloadDir
	}
	return base
}
