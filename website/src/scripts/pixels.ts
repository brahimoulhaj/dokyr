// The hero's pixel field: square cells on a grid, dense at the edges and the bottom and sparse
// behind the text, twinkling in shades of the theme's accent. The pointer lights the cells
// around it and they fade behind it. Colours are read from the theme tokens, so it follows
// the theme switch. The text, the wordmark and the buttons keep a clear margin around them.
// It stops off-screen and in a hidden tab, and draws one still frame under reduced motion.

type Cell = { x: number; y: number; base: number; level: number; target: number; heat: number; tone: number };

export function startPixels(canvas: HTMLCanvasElement, keepClear: HTMLElement[]) {
  const ctx = canvas.getContext('2d');
  if (!ctx) return;
  const host = canvas.parentElement as HTMLElement;
  const root = document.documentElement;
  const reduce = matchMedia('(prefers-reduced-motion: reduce)').matches;

  const CELL = 16;
  const SIZE = 12;
  let cells: Cell[] = [];
  let cols = 0;
  let rows = 0;
  let dpr = 1;
  let accent = '#9ece6a';
  let paper = '#0c0e12';
  let ink = '#d5d9e2';

  const readColors = () => {
    const s = getComputedStyle(root);
    accent = s.getPropertyValue('--color-accent').trim() || accent;
    paper = s.getPropertyValue('--color-paper').trim() || paper;
    ink = s.getPropertyValue('--color-ink').trim() || ink;
  };

  // How likely a cell is to be lit at rest: none in the middle, a scatter toward the sides
  // and the bottom corners, thinner on a phone.
  let narrow = false;
  const density = (u: number, v: number) => {
    const dx = Math.abs(u - 0.5) * 2;
    const side = dx ** 6 * 0.42;
    const floor = v ** 7 * (0.1 + 0.35 * dx);
    return Math.min(0.45, side + floor) * (narrow ? 0.5 : 1);
  };

  // Cells under the hero's text and controls, plus a margin, never light.
  let blocked = new Uint8Array(0);
  const MARGIN = 28;
  const measureClear = () => {
    blocked = new Uint8Array(cols * rows);
    const h = host.getBoundingClientRect();
    for (const el of keepClear) {
      const r = el.getBoundingClientRect();
      const x0 = Math.max(0, Math.floor((r.left - h.left - MARGIN) / CELL));
      const x1 = Math.min(cols - 1, Math.floor((r.right - h.left + MARGIN) / CELL));
      const y0 = Math.max(0, Math.floor((r.top - h.top - MARGIN) / CELL));
      const y1 = Math.min(rows - 1, Math.floor((r.bottom - h.top + MARGIN) / CELL));
      for (let y = y0; y <= y1; y++) for (let x = x0; x <= x1; x++) blocked[y * cols + x] = 1;
    }
  };

  let seed = 11;
  const rand = () => ((seed = (seed * 16807) % 2147483647) / 2147483647);

  const layout = () => {
    const r = host.getBoundingClientRect();
    dpr = Math.min(window.devicePixelRatio || 1, 2);
    canvas.width = Math.round(r.width * dpr);
    canvas.height = Math.round(r.height * dpr);
    cols = Math.ceil(r.width / CELL);
    rows = Math.ceil(r.height / CELL);
    narrow = r.width < 600;
    measureClear();
    seed = 11;
    cells = [];
    for (let y = 0; y < rows; y++) {
      for (let x = 0; x < cols; x++) {
        const p = blocked[y * cols + x] ? 0 : density(x / cols, y / rows);
        const base = rand() < p ? 0.25 + rand() * 0.6 : 0;
        cells.push({ x, y, base, level: base, target: base, heat: 0, tone: rand() });
      }
    }
  };

  // Accent mixed toward the paper (dim) or the ink (bright). Theme tokens are hex colours.
  const rgb = (h: string) => {
    const m = h.replace('#', '').match(/^([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i);
    return m ? m.slice(1).map((v) => parseInt(v, 16)) : [128, 128, 128];
  };
  const mix = (a: string, b: string, t: number) => {
    const A = rgb(a);
    const B = rgb(b);
    return `rgb(${A.map((v, i) => Math.round(v + (B[i] - v) * t)).join(',')})`;
  };
  let palette: string[] = [];
  let strength = 1;
  const buildPalette = () => {
    // Light papers make the accent loud: hold the field back there.
    const [r, g, b] = rgb(paper);
    strength = 0.2126 * r + 0.7152 * g + 0.0722 * b > 140 ? 0.55 : 0.85;
    palette = [mix(accent, paper, 0.55), mix(accent, paper, 0.35), mix(accent, paper, 0.15), accent, mix(accent, ink, 0.35)];
  };

  const draw = () => {
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, canvas.width, canvas.height);
    const off = (CELL - SIZE) / 2;
    for (const c of cells) {
      const a = Math.min(1, c.level + c.heat) * strength;
      if (a < 0.02) continue;
      ctx.globalAlpha = a;
      ctx.fillStyle = c.heat > 0.15 ? palette[4] : palette[Math.floor(c.tone * 4)];
      ctx.fillRect(c.x * CELL + off, c.y * CELL + off, SIZE, SIZE);
    }
    ctx.globalAlpha = 1;
  };

  // Pointer: light the cells near it.
  let px = -1e4;
  let py = -1e4;
  const RADIUS = 40;
  host.addEventListener('pointermove', (e) => {
    const r = host.getBoundingClientRect();
    px = e.clientX - r.left;
    py = e.clientY - r.top;
  });
  host.addEventListener('pointerleave', () => {
    px = py = -1e4;
  });

  let running = false;
  let seen = true;
  let raf = 0;
  let last = 0;

  const step = (now: number) => {
    const dt = Math.min(64, now - (last || now));
    last = now;
    // Twinkle: a few lit cells dim or brighten now and then; empty cells stay empty.
    const flips = Math.max(1, Math.round(cells.length * 0.004));
    for (let i = 0; i < flips; i++) {
      const c = cells[Math.floor(Math.random() * cells.length)];
      if (c.base > 0) c.target = Math.random() < 0.35 ? c.base * 0.15 : c.base * (0.6 + Math.random() * 0.6);
    }
    const cx0 = Math.floor((px - RADIUS) / CELL);
    const cx1 = Math.ceil((px + RADIUS) / CELL);
    const cy0 = Math.floor((py - RADIUS) / CELL);
    const cy1 = Math.ceil((py + RADIUS) / CELL);
    for (let y = Math.max(0, cy0); y <= Math.min(rows - 1, cy1); y++) {
      for (let x = Math.max(0, cx0); x <= Math.min(cols - 1, cx1); x++) {
        const d = Math.hypot(x * CELL + CELL / 2 - px, y * CELL + CELL / 2 - py);
        if (d < RADIUS && !blocked[y * cols + x] && Math.random() < 0.12 * (1 - d / RADIUS)) {
          const c = cells[y * cols + x];
          c.heat = Math.max(c.heat, 0.55 * (1 - d / RADIUS) + 0.15);
        }
      }
    }
    const ease = 1 - Math.pow(0.004, dt / 1000);
    const cool = Math.pow(0.04, dt / 1000);
    for (const c of cells) {
      c.level += (c.target - c.level) * ease;
      if (c.heat > 0) c.heat = c.heat * cool < 0.01 ? 0 : c.heat * cool;
    }
    draw();
    raf = requestAnimationFrame(step);
  };

  const sync = () => {
    const run = seen && !document.hidden && !reduce;
    if (run && !running) {
      running = true;
      last = 0;
      raf = requestAnimationFrame(step);
    } else if (!run && running) {
      running = false;
      cancelAnimationFrame(raf);
    }
  };

  readColors();
  buildPalette();
  layout();
  draw();

  new ResizeObserver(() => {
    layout();
    draw();
  }).observe(host);
  new IntersectionObserver(([e]) => {
    seen = e.isIntersecting;
    sync();
  }).observe(host);
  document.addEventListener('visibilitychange', sync);
  new MutationObserver(() => {
    readColors();
    buildPalette();
    draw();
  }).observe(root, { attributes: true, attributeFilter: ['data-theme'] });
  // Fonts change the text's size after the first layout.
  document.fonts?.ready.then(() => {
    layout();
    draw();
  });
  sync();
}
