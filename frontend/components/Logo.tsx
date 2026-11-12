export function Logo() {
  return (
    <span className="inline-flex items-center gap-2 text-sm font-medium tracking-wide text-white">
      <svg viewBox="0 0 32 32" className="h-7 w-7" aria-hidden="true">
        <rect width="32" height="32" rx="8" fill="#0284c7" />
        <text
          x="16"
          y="21"
          textAnchor="middle"
          fontFamily="Arial, sans-serif"
          fontSize="12"
          fontWeight="700"
          fill="white"
        >
          GO
        </text>
      </svg>
      GOmeet
    </span>
  );
}
