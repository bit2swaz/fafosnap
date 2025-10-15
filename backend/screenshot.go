package main

import (
	"context"
	"time"

	"github.com/chromedp/chromedp"
)

func takeScreenshot(url string, timeoutSeconds int) ([]byte, error) {
	if timeoutSeconds <= 0 {
		timeoutSeconds = 30
	}

	ctx, cancel := chromedp.NewContext(context.Background())
	defer cancel()

	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer timeoutCancel()

	var buf []byte
	tasks := chromedp.Tasks{
		chromedp.EmulateViewport(1920, 1080, chromedp.EmulateScale(2.0)),
		chromedp.Navigate(url),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Evaluate(`Array.from(document.querySelectorAll('img')).forEach(img => {
			if (img.loading === 'lazy') img.loading = 'eager';
			if (img.dataset.src) img.src = img.dataset.src;
			if (img.dataset.lazySrc) img.src = img.dataset.lazySrc;
			if (img.dataset.srcset) img.srcset = img.dataset.srcset;
		});`, nil),
		chromedp.ActionFunc(func(ctx context.Context) error {
			script := `(async () => {
				const delay = ms => new Promise(res => setTimeout(res, ms));
				let current = 0;
				let totalHeight = document.body.scrollHeight;
				const viewport = window.innerHeight;
				while (current < totalHeight) {
					window.scrollTo(0, current);
					await delay(200);
					current += Math.max(200, viewport / 2);
					totalHeight = document.body.scrollHeight;
				}
				window.scrollTo(0, 0);
			})()`
			return chromedp.Evaluate(script, nil).Do(ctx)
		}),
		chromedp.Sleep(750 * time.Millisecond),
		chromedp.FullScreenshot(&buf, 100),
	}

	if err := chromedp.Run(timeoutCtx, tasks...); err != nil {
		return nil, err
	}

	return buf, nil
}
