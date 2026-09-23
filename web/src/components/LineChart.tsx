import { useId, useLayoutEffect, useMemo, useRef, useState } from "react";
import type { Point } from "../lib/types";
import { group, timeOfDay } from "../lib/format";
import { useMeasure } from "../lib/useMeasure";

type Props = {
  points: Point[];
  unit: string;
  /** Names what is plotted; a single series needs no legend box. */
  title: string;
  height?: number;
};

const PAD = { top: 12, right: 52, bottom: 22, left: 0 };

const NONE: Point[] = [];

/** How long the line takes to slide one sample to the left. */
const STEP_MS = 1000;

type Seg = {
  x0: number;
  x1: number;
  y0: number;
  c1: number;
  c2: number;
  y1: number;
};

/**
 * A single-series line with a hairline grid, a crosshair on hover and the
 * current value labelled at the end. One series means one colour and no
 * legend: the title above the chart already says what is plotted.
 *
 * Every value is also in the table below the chart, so the data is never
 * gated behind a hover no keyboard or screen reader can perform.
 */
export function LineChart({ points, unit, title, height = 190 }: Props) {
  const [wrapRef, width] = useMeasure<HTMLDivElement>();
  const [hover, setHover] = useState<number | null>(null);
  const clipId = useId();
  const moverRef = useRef<SVGGElement>(null);
  const dotRef = useRef<SVGCircleElement>(null);
  const leftover = useRef(0);

  // A live series arrives once a second as the same window moved on by a
  // sample. Keep the samples that just fell off the left edge so the line
  // can slide over to its new place instead of jumping there.
  const [seen, setSeen] = useState({ points, lead: [] as Point[] });
  if (seen.points !== points) {
    setSeen({ points, lead: fallenOff(seen.points, points) });
  }
  const lead = seen.points === points ? seen.lead : NONE;

  const geom = useMemo(() => {
    if (width <= 0 || points.length < 2) return null;

    const values = points.map((p) => p.v);
    const rawMin = Math.min(...values);
    const rawMax = Math.max(...values);
    // Clean tick numbers beat a tight fit: the reader is comparing the
    // curve against round figures, not against its own extremes.
    const step = niceStep((rawMax - rawMin) / 3 || rawMax / 3 || 1);
    const min = Math.floor(rawMin / step) * step;
    const max = Math.ceil(rawMax / step) * step;
    const span = max - min || 1;

    const plotW = Math.max(1, width - PAD.left - PAD.right);
    const plotH = Math.max(1, height - PAD.top - PAD.bottom);
    const stepPx = plotW / (points.length - 1);
    const x = (i: number) => PAD.left + i * stepPx;
    const y = (v: number) => PAD.top + (1 - (v - min) / span) * plotH;

    const all = [...lead, ...points].map((p, i) => ({
      x: x(i - lead.length),
      y: y(p.v),
    }));
    const coords = all.slice(lead.length);
    const segs = monotone(all);
    const f = (n: number) => n.toFixed(2);
    const line =
      `M${f(all[0]!.x)} ${f(all[0]!.y)}` +
      segs
        .map((s) => {
          const dx = (s.x1 - s.x0) / 3;
          return ` C${f(s.x0 + dx)} ${f(s.c1)} ${f(s.x1 - dx)} ${f(s.c2)} ${f(s.x1)} ${f(s.y1)}`;
        })
        .join("");
    const base = f(PAD.top + plotH);
    const area = `${line} L${f(all[all.length - 1]!.x)} ${base} L${f(all[0]!.x)} ${base} Z`;

    const ticks: number[] = [];
    for (let v = min; v <= max + step / 2; v += step) ticks.push(round2(v));

    return {
      coords,
      segs,
      line,
      area,
      ticks,
      y,
      plotH,
      plotW,
      stepPx,
      shift: lead.length,
    };
  }, [points, lead, width, height]);

  // Slide the new line in from where the old one stood. The frames touch
  // the DOM directly: re-rendering the whole chart 60 times a second
  // would cost far more than moving one group and one dot.
  useLayoutEffect(() => {
    const mover = moverRef.current;
    const dot = dotRef.current;
    if (!geom || !mover) return;
    const reduce = window.matchMedia?.(
      "(prefers-reduced-motion: reduce)",
    ).matches;
    const start = reduce ? 0 : geom.shift * geom.stepPx + leftover.current;
    const right = PAD.left + geom.plotW;
    const place = (offset: number) => {
      leftover.current = offset;
      mover.setAttribute("transform", `translate(${offset.toFixed(2)} 0)`);
      dot?.setAttribute("cy", curveY(geom.segs, right - offset).toFixed(2));
    };
    place(start);
    if (start <= 0) return;
    const duration = (start / geom.stepPx) * STEP_MS;
    const t0 = performance.now();
    let frame = requestAnimationFrame(function tick(now) {
      const done = Math.min(1, (now - t0) / duration);
      place(start * (1 - done));
      if (done < 1) frame = requestAnimationFrame(tick);
    });
    return () => cancelAnimationFrame(frame);
  }, [geom]);

  const active = hover !== null ? points[hover] : null;

  function onMove(event: React.PointerEvent<HTMLDivElement>) {
    if (!geom) return;
    const rect = event.currentTarget.getBoundingClientRect();
    const ratio = (event.clientX - rect.left - PAD.left) / geom.plotW;
    const index = Math.round(ratio * (points.length - 1));
    setHover(Math.min(points.length - 1, Math.max(0, index)));
  }

  const lastCoord = geom?.coords[geom.coords.length - 1];
  const lastPoint = points[points.length - 1];

  return (
    <figure className="chart" style={{ margin: 0 }}>
      <div
        ref={wrapRef}
        onPointerMove={onMove}
        onPointerLeave={() => setHover(null)}
        style={{ position: "relative", touchAction: "pan-y" }}
      >
        {geom && (
          <svg
            width={width}
            height={height}
            viewBox={`0 0 ${width} ${height}`}
            role="img"
            aria-label={`${title}. Значения перечислены в таблице под графиком.`}
            focusable="false"
          >
            {geom.ticks.map((t) => (
              <g key={t}>
                <line
                  x1={0}
                  x2={width - PAD.right + 8}
                  y1={geom.y(t)}
                  y2={geom.y(t)}
                  stroke="var(--chart-grid)"
                  strokeWidth={1}
                  shapeRendering="crispEdges"
                />
                <text
                  x={width - PAD.right + 14}
                  y={geom.y(t)}
                  dominantBaseline="middle"
                  fill="var(--ink-muted)"
                  fontSize={11}
                  style={{ fontVariantNumeric: "tabular-nums" }}
                >
                  {group(t)}
                </text>
              </g>
            ))}

            <clipPath id={clipId}>
              <rect x={PAD.left} y={0} width={geom.plotW} height={height} />
            </clipPath>
            <g clipPath={`url(#${clipId})`}>
              <g ref={moverRef}>
                <path d={geom.area} fill="var(--chart-wash)" />
                <path
                  d={geom.line}
                  fill="none"
                  stroke="var(--chart-ink)"
                  strokeWidth={2}
                  strokeLinecap="round"
                  strokeLinejoin="round"
                />
              </g>
            </g>

            {active && hover !== null && geom.coords[hover] && (
              <g>
                <line
                  x1={geom.coords[hover].x}
                  x2={geom.coords[hover].x}
                  y1={PAD.top}
                  y2={PAD.top + geom.plotH}
                  stroke="var(--line-strong)"
                  strokeWidth={1}
                  shapeRendering="crispEdges"
                />
                <circle
                  cx={geom.coords[hover].x}
                  cy={geom.coords[hover].y}
                  r={4}
                  fill="var(--chart-ink)"
                  stroke="var(--surface)"
                  strokeWidth={2}
                />
              </g>
            )}

            {lastCoord && hover === null && (
              <circle
                ref={dotRef}
                cx={lastCoord.x}
                cy={lastCoord.y}
                r={4}
                fill="var(--chart-ink)"
                stroke="var(--surface)"
                strokeWidth={2}
              />
            )}

            <text
              x={0}
              y={height - 5}
              fill="var(--ink-muted)"
              fontSize={11}
              style={{ fontVariantNumeric: "tabular-nums" }}
            >
              {timeOfDay(points[0]!.t)}
            </text>
            <text
              x={width - PAD.right}
              y={height - 5}
              textAnchor="end"
              fill="var(--ink-muted)"
              fontSize={11}
              style={{ fontVariantNumeric: "tabular-nums" }}
            >
              {timeOfDay(points[points.length - 1]!.t)}
            </text>
          </svg>
        )}

        {!geom && <div className="skeleton" style={{ height }} />}

        {active && hover !== null && geom?.coords[hover] && (
          <div
            className="tooltip"
            style={{
              left: `${geom.coords[hover].x}px`,
              top: `${geom.coords[hover].y - 10}px`,
            }}
          >
            <div className="tooltip__label">{timeOfDay(active.t)}</div>
            <div className="tooltip__value">
              {group(active.v)} {unit}
            </div>
          </div>
        )}
      </div>

      <details className="chart__table">
        <summary>
          Значения таблицей
          {lastPoint ? ` — сейчас ${group(lastPoint.v)} ${unit}` : ""}
        </summary>
        <div
          className="table-wrap"
          tabIndex={0}
          role="region"
          aria-label={title}
          style={{ marginTop: 10, maxHeight: 220, overflowY: "auto" }}
        >
          <table className="data">
            <caption className="visually-hidden">{title}</caption>
            <thead>
              <tr>
                <th scope="col">Время</th>
                <th scope="col" className="num">
                  {unit}
                </th>
              </tr>
            </thead>
            <tbody>
              {points.map((p) => (
                <tr key={p.t}>
                  <td>{timeOfDay(p.t)}</td>
                  <td className="num">{group(p.v)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </details>
    </figure>
  );
}

/**
 * The samples that left the window since the last update, oldest first,
 * or none when the new series is not the old one moved on by a few steps
 * (first load, a tab back from the background, a different series).
 */
function fallenOff(prev: Point[], next: Point[]): Point[] {
  const first = next[0];
  if (!first || prev.length !== next.length) return [];
  const k = prev.findIndex((p) => p.t === first.t);
  return k > 0 && k <= 3 ? prev.slice(0, k) : [];
}

/**
 * Monotone cubic through the points (Fritsch–Carlson): smooth, yet it
 * never overshoots a sample, so the curve shows no peak or dip that was
 * not measured. Control points sit at thirds of each step, which keeps x
 * linear in the curve parameter and makes curveY exact.
 */
function monotone(pts: { x: number; y: number }[]): Seg[] {
  const n = pts.length;
  const m: number[] = [];
  for (let i = 0; i < n - 1; i++) {
    m.push((pts[i + 1]!.y - pts[i]!.y) / (pts[i + 1]!.x - pts[i]!.x || 1));
  }
  const tan = pts.map((_, i) => {
    if (i === 0) return m[0] ?? 0;
    if (i === n - 1) return m[n - 2] ?? 0;
    const a = m[i - 1]!;
    const b = m[i]!;
    return a * b <= 0 ? 0 : (2 * a * b) / (a + b);
  });
  const segs: Seg[] = [];
  for (let i = 0; i < n - 1; i++) {
    const p = pts[i]!;
    const q = pts[i + 1]!;
    const dx = (q.x - p.x) / 3;
    segs.push({
      x0: p.x,
      x1: q.x,
      y0: p.y,
      c1: p.y + tan[i]! * dx,
      c2: q.y - tan[i + 1]! * dx,
      y1: q.y,
    });
  }
  return segs;
}

/** The curve's height at x, so the end dot rides the line as it slides. */
function curveY(segs: Seg[], x: number): number {
  const s = segs.find((g) => x <= g.x1) ?? segs[segs.length - 1];
  if (!s) return 0;
  const t = Math.min(1, Math.max(0, (x - s.x0) / (s.x1 - s.x0 || 1)));
  const u = 1 - t;
  return (
    u * u * u * s.y0 +
    3 * u * u * t * s.c1 +
    3 * u * t * t * s.c2 +
    t * t * t * s.y1
  );
}

/** 1, 2, 5 × 10ⁿ — the steps people read without doing arithmetic. */
function niceStep(raw: number): number {
  const magnitude = 10 ** Math.floor(Math.log10(Math.abs(raw) || 1));
  const scaled = raw / magnitude;
  const nice = scaled <= 1 ? 1 : scaled <= 2 ? 2 : scaled <= 5 ? 5 : 10;
  return nice * magnitude;
}

function round2(n: number): number {
  return Math.round(n * 100) / 100;
}
