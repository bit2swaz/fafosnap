package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

func takeScreenshot(url string, timeoutSeconds int) ([]byte, error) {
	if timeoutSeconds <= 0 {
		timeoutSeconds = 60 // Increased default for GitHub's heavy page
	}

	ctx, cancel := chromedp.NewContext(context.Background())
	defer cancel()

	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer timeoutCancel()

	var buf []byte
	tasks := chromedp.Tasks{
		chromedp.EmulateViewport(1920, 1080),
		chromedp.Navigate(url),

		// Wait for initial page load
		chromedp.WaitReady("body", chromedp.ByQuery),

		// Wait for DOM to be complete
		chromedp.ActionFunc(func(ctx context.Context) error {
			for i := 0; i < 50; i++ {
				var ready string
				if err := chromedp.Evaluate(`document.readyState`, &ready).Do(ctx); err != nil {
					return err
				}
				if ready == "complete" {
					return nil
				}
				time.Sleep(200 * time.Millisecond)
			}
			return nil
		}),

		// Initial wait for SPAs and network requests to settle
		chromedp.Sleep(3 * time.Second),

		// Disable smooth scrolling for consistent behavior
		chromedp.Evaluate(`
			document.documentElement.style.scrollBehavior = 'auto';
			document.body.style.scrollBehavior = 'auto';
		`, nil),

		// Force eager loading of all lazy-loaded images before scrolling
		chromedp.Evaluate(`
			Array.from(document.querySelectorAll('img')).forEach(img => {
				if (img.loading === 'lazy') img.loading = 'eager';
				if (img.dataset.src) img.src = img.dataset.src;
				if (img.dataset.lazySrc) img.src = img.dataset.lazySrc;
				if (img.dataset.srcset) img.srcset = img.dataset.srcset;
			});
			Array.from(document.querySelectorAll('source')).forEach(src => {
				if (src.dataset.srcset) src.srcset = src.dataset.srcset;
			});
		`, nil),

		chromedp.Sleep(1 * time.Second),

		// ONE complete scroll through the page to trigger all animations and lazy content
		chromedp.ActionFunc(func(ctx context.Context) error {
			script := `(async () => {
				const delay = ms => new Promise(res => setTimeout(res, ms));
				
				// Get initial measurements
				let scrollHeight = document.documentElement.scrollHeight;
				const viewportHeight = window.innerHeight;
				
				// Scroll down in smooth steps to trigger all viewport-based animations
				let currentScroll = 0;
				const scrollStep = Math.floor(viewportHeight * 0.7); // 70% of viewport per step
				
				while (currentScroll < scrollHeight - viewportHeight) {
					currentScroll += scrollStep;
					
					// Don't scroll past the bottom
					if (currentScroll > scrollHeight - viewportHeight) {
						currentScroll = scrollHeight - viewportHeight;
					}
					
					window.scrollTo(0, currentScroll);
					window.dispatchEvent(new Event('scroll'));
					
					// Wait for animations to trigger and start
					await delay(600);
					
					// Recalculate in case content expanded
					scrollHeight = document.documentElement.scrollHeight;
				}
				
				// Ensure we hit the absolute bottom
				window.scrollTo(0, document.documentElement.scrollHeight);
				window.dispatchEvent(new Event('scroll'));
				await delay(1000);
				
				// Now scroll back up in larger steps (animations already triggered)
				currentScroll = document.documentElement.scrollHeight;
				const scrollUpStep = Math.floor(viewportHeight * 1.5);
				
				while (currentScroll > 0) {
					currentScroll -= scrollUpStep;
					if (currentScroll < 0) currentScroll = 0;
					
					window.scrollTo(0, currentScroll);
					window.dispatchEvent(new Event('scroll'));
					await delay(400);
				}
				
				// Final scroll to absolute top
				window.scrollTo(0, 0);
				window.dispatchEvent(new Event('scroll'));
				await delay(800);
			})()`
			return chromedp.Evaluate(script, nil).Do(ctx)
		}),

		// Wait for all network activity to settle
		chromedp.ActionFunc(func(ctx context.Context) error {
			script := `(async () => {
				const delay = ms => new Promise(res => setTimeout(res, ms));
				
				// Wait for any ongoing fetch requests
				if (window.performance && window.performance.getEntriesByType) {
					await delay(1000);
				}
			})()`
			return chromedp.Evaluate(script, nil).Do(ctx)
		}),

		// Wait for ALL images to complete loading (including dynamic ones)
		chromedp.ActionFunc(func(ctx context.Context) error {
			script := `(async () => {
				const delay = ms => new Promise(res => setTimeout(res, ms));
				
				// Function to wait for all images
				const waitForImages = () => {
					const images = Array.from(document.querySelectorAll('img'));
					const promises = images.map(img => {
						if (img.complete && img.naturalHeight !== 0) return Promise.resolve();
						return new Promise(resolve => {
							const done = () => {
								img.onload = null;
								img.onerror = null;
								resolve();
							};
							img.onload = done;
							img.onerror = done;
							setTimeout(done, 10000); // 10s timeout per image
						});
					});
					return Promise.all(promises);
				};
				
				// Wait for images multiple times to catch dynamically added ones
				await waitForImages();
				await delay(500);
				await waitForImages();
				await delay(500);
			})()`
			return chromedp.Evaluate(script, nil).Do(ctx)
		}),

		// Wait for custom fonts
		chromedp.ActionFunc(func(ctx context.Context) error {
			script := `document.fonts ? document.fonts.ready : Promise.resolve()`
			return chromedp.Evaluate(script, nil).Do(ctx)
		}),

		// Wait for all animations to complete
		chromedp.ActionFunc(func(ctx context.Context) error {
			script := `(async () => {
				const delay = ms => new Promise(res => setTimeout(res, ms));
				
				if (document.getAnimations) {
					// Get all running animations
					const animations = document.getAnimations();
					const runningAnims = animations.filter(anim => 
						anim.playState === 'running' || anim.playState === 'pending'
					);
					
					// Wait for them to finish (with timeout)
					const timeout = new Promise(res => setTimeout(res, 5000));
					const allFinished = Promise.allSettled(runningAnims.map(a => a.finished));
					await Promise.race([allFinished, timeout]);
				}
				
				// Extra buffer for any CSS transitions
				await delay(1000);
			})()`
			return chromedp.Evaluate(script, nil).Do(ctx)
		}),

		// Ensure navbar and fixed elements are properly positioned
		chromedp.ActionFunc(func(ctx context.Context) error {
			script := `(async () => {
				const delay = ms => new Promise(res => setTimeout(res, ms));
				
				// Force repaint
				document.body.style.display = 'none';
				document.body.offsetHeight; // Trigger reflow
				document.body.style.display = '';
				
				// Double RAF to ensure layout is stable
				await new Promise(res => requestAnimationFrame(res));
				await new Promise(res => requestAnimationFrame(res));
				await delay(300);
			})()`
			return chromedp.Evaluate(script, nil).Do(ctx)
		}),

		// Final verification that we're at the top
		chromedp.Evaluate(`window.scrollTo(0, 0)`, nil),
		chromedp.Sleep(800 * time.Millisecond),

		// One more RAF to ensure everything is painted
		chromedp.ActionFunc(func(ctx context.Context) error {
			script := `new Promise(res => requestAnimationFrame(() => requestAnimationFrame(res)))`
			return chromedp.Evaluate(script, nil).Do(ctx)
		}),

		// Capture stitched screenshot and return raw PNG data
		chromedp.ActionFunc(func(ctx context.Context) error {
			data, err := captureStitchedScreenshot(ctx)
			if err != nil {
				return err
			}
			buf = data
			return nil
		}),
	}

	if err := chromedp.Run(timeoutCtx, tasks...); err != nil {
		return nil, err
	}

	return buf, nil
}

func captureStitchedScreenshot(ctx context.Context) ([]byte, error) {
	var metrics struct {
		Width    float64 `json:"width"`
		Height   float64 `json:"height"`
		Viewport float64 `json:"viewport"`
		DPR      float64 `json:"dpr"`
	}

	metricsScript := `({
		width: Math.max(document.documentElement.scrollWidth, window.innerWidth),
		height: Math.max(document.documentElement.scrollHeight, window.innerHeight),
		viewport: window.innerHeight,
		dpr: window.devicePixelRatio || 1
	})`

	if err := chromedp.Run(ctx, chromedp.Evaluate(metricsScript, &metrics, chromedp.EvalAsValue)); err != nil {
		return nil, fmt.Errorf("collect layout metrics: %w", err)
	}

	if metrics.Width == 0 || metrics.Height == 0 {
		return nil, fmt.Errorf("page dimensions unavailable (width %.2f height %.2f)", metrics.Width, metrics.Height)
	}

	scaleFactor := math.Max(metrics.DPR, 1.0)
	const targetScale = 2.0
	const maxScaledArea = 2.5e8 // guard against extreme memory usage (~500 MP)
	if scaleFactor < targetScale {
		projected := metrics.Width * metrics.Height * targetScale * targetScale
		if projected <= maxScaledArea {
			scaleFactor = targetScale
		}
	}

	chunkHeight := metrics.Viewport
	if chunkHeight <= 0 {
		chunkHeight = 1080
	}
	chunkHeight = math.Min(chunkHeight, 2000)

	type chunk struct {
		top int
		img *image.NRGBA
	}

	var (
		chunks      []chunk
		totalHeight int
		maxWidth    int
	)

	for offset := 0.0; offset < metrics.Height; {
		captureHeight := math.Min(chunkHeight, metrics.Height-offset)
		if captureHeight <= 0 {
			break
		}

		clip := &page.Viewport{
			X:      0,
			Y:      offset,
			Width:  metrics.Width,
			Height: captureHeight,
			Scale:  scaleFactor,
		}

		capture, err := page.CaptureScreenshot().
			WithFormat(page.CaptureScreenshotFormatPng).
			WithClip(clip).
			WithFromSurface(true).
			Do(ctx)
		if err != nil {
			return nil, fmt.Errorf("capture chunk @%.2f: %w", offset, err)
		}

		imgChunk, err := png.Decode(bytes.NewReader(capture))
		if err != nil {
			return nil, fmt.Errorf("parse chunk png @%.2f: %w", offset, err)
		}

		normalized := toNRGBA(imgChunk)
		chunkHeightPx := normalized.Bounds().Dy()
		chunkWidthPx := normalized.Bounds().Dx()
		if chunkHeightPx == 0 || chunkWidthPx == 0 {
			offset += captureHeight
			continue
		}

		chunks = append(chunks, chunk{
			top: totalHeight,
			img: normalized,
		})
		totalHeight += chunkHeightPx
		if chunkWidthPx > maxWidth {
			maxWidth = chunkWidthPx
		}

		offset += captureHeight
	}

	if len(chunks) == 0 || maxWidth == 0 || totalHeight == 0 {
		return nil, fmt.Errorf("no screenshot chunks captured")
	}

	stitched := image.NewNRGBA(image.Rect(0, 0, maxWidth, totalHeight))
	for _, c := range chunks {
		dest := image.Rect(0, c.top, c.img.Bounds().Dx(), c.top+c.img.Bounds().Dy())
		draw.Draw(stitched, dest, c.img, image.Point{}, draw.Src)
	}

	var buffer bytes.Buffer
	if err := png.Encode(&buffer, stitched); err != nil {
		return nil, fmt.Errorf("encode stitched screenshot: %w", err)
	}

	return buffer.Bytes(), nil
}

func toNRGBA(img image.Image) *image.NRGBA {
	if existing, ok := img.(*image.NRGBA); ok && existing.Bounds().Min == (image.Point{}) {
		return existing
	}

	bounds := img.Bounds()
	normalized := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(normalized, normalized.Bounds(), img, bounds.Min, draw.Src)
	return normalized
}
