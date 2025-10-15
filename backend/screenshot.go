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
		chromedp.Navigate(url),
		chromedp.WaitVisible("body", chromedp.ByQuery),
		chromedp.FullScreenshot(&buf, 90),
	}

	if err := chromedp.Run(timeoutCtx, tasks...); err != nil {
		return nil, err
	}

	return buf, nil
}
