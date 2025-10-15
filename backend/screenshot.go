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
		chromedp.EmulateViewport(1920, 1080),
		chromedp.Navigate(url),
		chromedp.WaitReady("body", chromedp.ByQuery),
		// Wait for DOM readyState to reach "complete"
		chromedp.ActionFunc(func(ctx context.Context) error {
			for i := 0; i < 40; i++ {
				var ready string
				if err := chromedp.Evaluate(`document.readyState`, &ready).Do(ctx); err != nil {
					return err
				}
				if ready == "complete" {
					return nil
				}
				time.Sleep(250 * time.Millisecond)
			}
			return nil
		}),
		// Allow SPA hydration/network bursts to finish
		chromedp.Sleep(4 * time.Second),
		// Force eager loading of lazy-loaded images and handle various lazy-loading patterns
		chromedp.Evaluate(`
			// Handle standard lazy loading
			Array.from(document.querySelectorAll('img')).forEach(img => {
				if (img.loading === 'lazy') img.loading = 'eager';
				if (img.dataset.src) img.src = img.dataset.src;
				if (img.dataset.lazySrc) img.src = img.dataset.lazySrc;
				if (img.dataset.srcset) img.srcset = img.dataset.srcset;
			});
			// Handle source elements
			Array.from(document.querySelectorAll('source')).forEach(src => {
				if (src.dataset.srcset) src.srcset = src.dataset.srcset;
			});
			// Handle picture elements
			Array.from(document.querySelectorAll('picture')).forEach(pic => {
				const img = pic.querySelector('img');
				if (img && img.dataset.src) img.src = img.dataset.src;
			});
		`, nil),
		chromedp.Sleep(1 * time.Second),
		// Scroll through the page to trigger lazy-loaded content
		chromedp.ActionFunc(func(ctx context.Context) error {
			script := `(async () => {
				const delay = ms => new Promise(res => setTimeout(res, ms));
				const originalBehavior = document.documentElement.style.scrollBehavior;
				document.documentElement.style.scrollBehavior = 'auto';
				
				let viewport = window.innerHeight;
				let scrollHeight = document.body.scrollHeight;
				let current = 0;
				const downStep = Math.max(200, viewport / 2);
				
				while (current + viewport < scrollHeight) {
					current = Math.min(current + downStep, scrollHeight - viewport);
					window.scrollTo(0, current);
					window.dispatchEvent(new Event('scroll'));
					await delay(360);
					scrollHeight = document.body.scrollHeight;
					viewport = window.innerHeight;
				}
				
				window.scrollTo(0, document.body.scrollHeight);
				window.dispatchEvent(new Event('scroll'));
				await delay(800);
				
				current = document.body.scrollHeight;
				const upStep = Math.max(200, viewport / 3);
				while (current > 0) {
					current = Math.max(0, current - upStep);
					window.scrollTo(0, current);
					window.dispatchEvent(new Event('scroll'));
					await delay(200);
				}
				
				window.scrollTo(0, 0);
				window.dispatchEvent(new Event('scroll'));
				await delay(600);
				
				if (document.getAnimations) {
					const animations = document.getAnimations().filter(anim => !anim.playState || anim.playState !== 'finished');
					await Promise.allSettled(animations.map(anim => anim.finished)).catch(() => {});
				}
				await new Promise(res => requestAnimationFrame(() => requestAnimationFrame(res)));
				
				document.documentElement.style.scrollBehavior = originalBehavior;
			})()`
			return chromedp.Evaluate(script, nil).Do(ctx)
		}),
		// Wait for images to complete loading
		chromedp.ActionFunc(func(ctx context.Context) error {
			script := `(async () => {
				const delay = ms => new Promise(res => setTimeout(res, ms));
				
				// Wait for all images to load
				const images = Array.from(document.querySelectorAll('img'));
				const imagePromises = images.map(img => {
					if (img.complete) return Promise.resolve();
					return new Promise(resolve => {
						const done = () => {
							img.onload = null;
							img.onerror = null;
							resolve();
						};
						img.onload = done;
						img.onerror = done; // Resolve even on error to not block
						// Timeout after 8 seconds per image
						setTimeout(done, 8000);
					});
				});
				
				await Promise.all(imagePromises);
				await delay(500); // Extra buffer for rendering
			})()`
			return chromedp.Evaluate(script, nil).Do(ctx)
		}),
		// Wait for custom fonts to finish loading
		chromedp.ActionFunc(func(ctx context.Context) error {
			script := `document.fonts ? document.fonts.ready : Promise.resolve()`
			return chromedp.Evaluate(script, nil).Do(ctx)
		}),
		// Final wait for any animations/transitions to complete
		chromedp.Sleep(2 * time.Second),
		// Ensure we're at the very top before screenshot and layout is stable
		chromedp.ActionFunc(func(ctx context.Context) error {
			script := `(async () => {
				const delay = ms => new Promise(res => setTimeout(res, ms));
				window.scrollTo(0, 0);
				window.dispatchEvent(new Event('scroll'));
				await delay(500);
				await new Promise(res => requestAnimationFrame(() => requestAnimationFrame(res)));
			})()`
			return chromedp.Evaluate(script, nil).Do(ctx)
		}),
		chromedp.Sleep(500 * time.Millisecond),
		chromedp.FullScreenshot(&buf, 98),
	}

	if err := chromedp.Run(timeoutCtx, tasks...); err != nil {
		return nil, err
	}

	return buf, nil
}
