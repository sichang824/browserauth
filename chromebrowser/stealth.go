package chromebrowser

import (
	"context"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// stealthInitScript hides common automation markers on every new document.
const stealthInitScript = `
if (navigator.webdriver === true) {
  Object.defineProperty(navigator, 'webdriver', { get: () => undefined });
}
window.chrome = window.chrome || { runtime: {} };
`

// InjectStealth registers scripts that reduce automation fingerprinting.
func InjectStealth(ctx context.Context) error {
	return chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		_, err := page.AddScriptToEvaluateOnNewDocument(stealthInitScript).Do(ctx)
		return err
	}))
}
