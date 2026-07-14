import { useEffect, useRef } from "react";
import { useTheme } from "@/theme";

// 4×4 Bayer matrix (normalized thresholds) for ordered dithering.
const BAYER = [
  [0, 8, 2, 10],
  [12, 4, 14, 6],
  [3, 11, 1, 9],
  [15, 7, 13, 5],
].map((row) => row.map((v) => (v + 0.5) / 16));

// A restrained, self-contained dithered backdrop: an ordered dither of a slow
// plasma field in neutral gray (matches the clean-minimal palette). Rendered on a
// downscaled buffer and scaled up with smoothing off for the classic chunky-dot
// look. Cheap and reduced-motion aware. Purely decorative — pointer-events: none.
export function DitherField({ className }: { className?: string }) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const { theme } = useTheme();

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    const cv = canvas; // non-null aliases keep narrowing inside the render closures
    const c = ctx;

    const dark = theme === "dark";
    // Neutral gray dots + peak alpha per theme (light stays fainter): light dots
    // on the dark ground, dark dots on the light ground.
    const [r, g, b] = dark ? [212, 212, 212] : [82, 82, 82];
    const peak = dark ? 0.14 : 0.07;

    const cell = 6; // upscaled dot size in CSS px
    let bw = 0;
    let bh = 0;
    let buffer: ImageData | null = null;
    const off = document.createElement("canvas");
    const offCtx = off.getContext("2d");

    function resize() {
      const rect = cv.getBoundingClientRect();
      cv.width = Math.max(1, Math.floor(rect.width));
      cv.height = Math.max(1, Math.floor(rect.height));
      bw = Math.max(1, Math.ceil(rect.width / cell));
      bh = Math.max(1, Math.ceil(rect.height / cell));
      off.width = bw;
      off.height = bh;
      buffer = offCtx ? offCtx.createImageData(bw, bh) : null;
      c.imageSmoothingEnabled = false;
    }
    resize();

    const reduce = window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;
    let raf = 0;
    let last = 0;

    function frame(now: number) {
      if (!buffer || !offCtx) return;
      // ~24fps cap to keep it gentle.
      if (now - last < 42) {
        raf = requestAnimationFrame(frame);
        return;
      }
      last = now;
      const t = now / 1000;
      const data = buffer.data;
      const cx = 0.5 + 0.28 * Math.sin(t * 0.13);
      const cy = 0.42 + 0.22 * Math.cos(t * 0.11);
      for (let y = 0; y < bh; y++) {
        const ny = y / bh;
        for (let x = 0; x < bw; x++) {
          const nx = x / bw;
          // moving plasma + radial falloff toward a drifting center
          const wave =
            0.5 +
            0.5 * Math.sin(nx * 7 + t * 0.6) * Math.cos(ny * 6 - t * 0.4);
          const dx = nx - cx;
          const dy = ny - cy;
          const radial = Math.max(0, 1 - Math.sqrt(dx * dx + dy * dy) * 1.5);
          const v = wave * 0.55 + radial * 0.6;
          const idx = (y * bw + x) * 4;
          if (v > BAYER[y & 3][x & 3]) {
            data[idx] = r;
            data[idx + 1] = g;
            data[idx + 2] = b;
            data[idx + 3] = Math.min(255, Math.floor(peak * 255 * Math.min(1, v)));
          } else {
            data[idx + 3] = 0;
          }
        }
      }
      offCtx.putImageData(buffer, 0, 0);
      c.clearRect(0, 0, cv.width, cv.height);
      c.drawImage(off, 0, 0, bw, bh, 0, 0, cv.width, cv.height);
      if (!reduce) raf = requestAnimationFrame(frame);
    }

    raf = requestAnimationFrame(frame);
    const ro = new ResizeObserver(() => {
      resize();
      if (reduce) requestAnimationFrame(frame);
    });
    ro.observe(canvas);

    return () => {
      cancelAnimationFrame(raf);
      ro.disconnect();
    };
  }, [theme]);

  return <canvas ref={canvasRef} aria-hidden className={className} style={{ width: "100%", height: "100%" }} />;
}
