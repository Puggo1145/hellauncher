package hud

import (
	"sync"

	"github.com/egoist/mygo/ui"
)

var uiIconSVGSources = map[string]string{
	"close_window": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none">
  <rect x="4" y="5" width="16" height="14" stroke="#eef1f6" stroke-width="1.8"/>
  <path d="M4 8.5 H20 M9.5 11.2 L14.5 16.2 M14.5 11.2 L9.5 16.2" stroke="#eef1f6" stroke-width="1.9" stroke-linecap="square"/>
</svg>`,
	"win_left": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none">
  <rect x="3.5" y="4.5" width="17" height="15" stroke="#eef1f6" stroke-width="1.8"/>
  <rect x="5" y="6" width="7" height="12" fill="#eef1f6" fill-opacity="0.9"/>
</svg>`,
	"win_right": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none">
  <rect x="3.5" y="4.5" width="17" height="15" stroke="#eef1f6" stroke-width="1.8"/>
  <rect x="12" y="6" width="7" height="12" fill="#eef1f6" fill-opacity="0.9"/>
</svg>`,
	"win_maximize": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none">
  <rect x="4" y="5" width="16" height="14" stroke="#eef1f6" stroke-width="1.8"/>
  <path d="M4 8.5 H20 M7.5 11.5 V16 H16.5 V11.5 Z" stroke="#eef1f6" stroke-width="1.7" fill="#eef1f6" fill-opacity="0.22"/>
</svg>`,
	"win_center": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none">
  <rect x="3.5" y="4.5" width="17" height="15" stroke="#eef1f6" stroke-opacity="0.45" stroke-width="1.6"/>
  <rect x="7" y="7.5" width="10" height="9" stroke="#eef1f6" stroke-width="1.8" fill="#eef1f6" fill-opacity="0.28"/>
</svg>`,
	"win_minimize": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none">
  <rect x="4" y="5" width="16" height="14" stroke="#eef1f6" stroke-opacity="0.45" stroke-width="1.6"/>
  <path d="M7 15.5 H17" stroke="#eef1f6" stroke-width="2.4" stroke-linecap="square"/>
</svg>`,
	"show_desktop": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none">
  <rect x="3.5" y="4.5" width="17" height="13" stroke="#eef1f6" stroke-width="1.8"/>
  <path d="M3.5 14.5 H20.5 M8 20 H16 M12 17.5 V20" stroke="#eef1f6" stroke-width="1.8" stroke-linecap="square"/>
</svg>`,
	"screenshot": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none">
  <path d="M4 8.5 V4.5 H8 M16 4.5 H20 V8.5 M20 15.5 V19.5 H16 M8 19.5 H4 V15.5" stroke="#eef1f6" stroke-width="1.9" stroke-linecap="square"/>
  <circle cx="12" cy="12" r="2.5" stroke="#eef1f6" stroke-width="1.8"/>
</svg>`,
	"lock_screen": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none">
  <rect x="5.5" y="10.5" width="13" height="9.5" stroke="#eef1f6" stroke-width="1.9"/>
  <path d="M8.5 10.5 V8 A3.5 3.5 0 0 1 15.5 8 V10.5" stroke="#eef1f6" stroke-width="1.9"/>
  <rect x="11.2" y="13.5" width="1.6" height="3.2" fill="#eef1f6"/>
</svg>`,
	"media_play": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none">
  <path d="M5 6.5 L12.5 12 L5 17.5 Z" fill="#eef1f6"/>
  <rect x="15" y="6.5" width="2.4" height="11" fill="#eef1f6"/>
  <rect x="19" y="6.5" width="2.4" height="11" fill="#eef1f6"/>
</svg>`,
	"volume_mute": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none">
  <path d="M4 9.5 H7.8 L12.5 5.5 V18.5 L7.8 14.5 H4 Z" fill="#eef1f6"/>
  <path d="M16 9 L20.5 15 M20.5 9 L16 15" stroke="#eef1f6" stroke-width="2" stroke-linecap="square"/>
</svg>`,
	"task_manager": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none">
  <rect x="3.5" y="4.5" width="17" height="15" stroke="#eef1f6" stroke-opacity="0.45" stroke-width="1.6"/>
  <path d="M5 13 H8.2 L10.2 8 L13.4 16 L15.4 11.5 H19" stroke="#eef1f6" stroke-width="2" stroke-linecap="square" stroke-linejoin="miter"/>
</svg>`,
	"empty_trash": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none">
  <path d="M5 7 H19 M9 7 V4.5 H15 V7 M7 7 L8 19.5 H16 L17 7 M10 10.5 V16 M14 10.5 V16" stroke="#eef1f6" stroke-width="1.8" stroke-linecap="square"/>
</svg>`,
	"speaker_on": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none">
  <path d="M4 9.5 H7.8 L12.5 5.5 V18.5 L7.8 14.5 H4 Z" fill="#eef1f6"/>
  <path d="M15.8 9.2 A4.2 4.2 0 0 1 15.8 14.8 M18.5 6.8 A7.4 7.4 0 0 1 18.5 17.2" stroke="#eef1f6" stroke-width="1.9" stroke-linecap="square"/>
</svg>`,
	"speaker_off": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none">
  <path d="M4 9.5 H7.8 L12.5 5.5 V18.5 L7.8 14.5 H4 Z" fill="#eef1f6"/>
  <path d="M16 9 L20.5 15 M20.5 9 L16 15" stroke="#eef1f6" stroke-width="2" stroke-linecap="square"/>
</svg>`,
	"reroll": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none">
  <path d="M4 8 H17 L14.5 5.5 M17 8 L14.5 10.5 M20 16 H7 L9.5 13.5 M7 16 L9.5 18.5" stroke="#eef1f6" stroke-width="2" stroke-linecap="square"/>
</svg>`,
	"bolt": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none">
  <path d="M13.5 3 L6 13.5 H11.5 L10.5 21 L18 10.5 H12.5 Z" fill="#eef1f6"/>
</svg>`,
	"hellpod": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none">
  <path d="M12 2.5 L17 7.5 V15.5 L12 21.5 L7 15.5 V7.5 Z" stroke="#16181c" stroke-width="2" fill="#16181c" fill-opacity="0.18"/>
  <path d="M9.5 8.5 H14.5 M12 10.5 V17.5" stroke="#16181c" stroke-width="2" stroke-linecap="square"/>
</svg>`,
	"reset_warn": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none">
  <path d="M12 3.5 L21 19.5 H3 Z" stroke="#ff7a70" stroke-width="1.9" stroke-linejoin="miter"/>
  <rect x="11.2" y="9.5" width="1.6" height="4.8" fill="#ff7a70"/>
  <rect x="11.2" y="15.8" width="1.6" height="1.6" fill="#ff7a70"/>
</svg>`,
	"json_export": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none">
  <path d="M5 15 V19 H19 V15 M12 15 V5 M8.5 8.5 L12 5 L15.5 8.5" stroke="#eef1f6" stroke-width="2" stroke-linecap="square"/>
</svg>`,
	"json_import": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none">
  <path d="M5 15 V19 H19 V15 M12 5 V15 M8.5 11.5 L12 15 L15.5 11.5" stroke="#eef1f6" stroke-width="2" stroke-linecap="square"/>
</svg>`,
}

var (
	uiIconSVGsOnce sync.Once
	uiIconSVGs     map[string]*ui.SVG
)

// UIIconSVG returns a cached monochrome tactical vector icon for Arsenal and
// Onboarding controls, avoiding platform-inconsistent colorful OS emojis.
func UIIconSVG(key string) *ui.SVG {
	uiIconSVGsOnce.Do(func() {
		uiIconSVGs = make(map[string]*ui.SVG, len(uiIconSVGSources))
		for k, src := range uiIconSVGSources {
			uiIconSVGs[k] = ui.MustParseSVG([]byte(src))
		}
	})
	return uiIconSVGs[key]
}
