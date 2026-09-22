/**
 * The mark is the product in one glyph: a payload (the filled square —
 * WireGuard's own ciphertext) sitting inside a second, outer envelope
 * (this project's layer). Nothing about the inner square is visible from
 * outside the outer one.
 */
export function Logo({ size = 20 }: { size?: number }) {
  return (
    <svg
      className="brand__mark"
      width={size}
      height={size}
      viewBox="0 0 20 20"
      aria-hidden="true"
      focusable="false"
    >
      <rect
        x="1"
        y="1"
        width="18"
        height="18"
        rx="5"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.6"
      />
      <rect x="6.5" y="6.5" width="7" height="7" rx="2" fill="currentColor" />
    </svg>
  )
}
