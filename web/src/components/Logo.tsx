interface LogoProps {
  className?: string
  showText?: boolean
}

export function Logo({ className = '', showText = true }: LogoProps) {
  return (
    <div className={`flex items-center gap-3 ${className}`}>
      <svg
        width="64"
        height="64"
        viewBox="0 0 32 32"
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
        className="shrink-0"
      >
        {/* Outer circle - gateway/portal */}
        <circle
          cx="16"
          cy="16"
          r="14"
          stroke="url(#gradient1)"
          strokeWidth="2"
          strokeDasharray="4 2"
          className="animate-spin-slow"
          style={{ animationDuration: '20s' }}
        />
        
        {/* Inner flowing paths - "let go" concept */}
        <path
          d="M8 16 Q12 10, 16 16 T24 16"
          stroke="url(#gradient2)"
          strokeWidth="2.5"
          strokeLinecap="round"
          fill="none"
          opacity="0.9"
        />
        <path
          d="M8 20 Q12 14, 16 20 T24 20"
          stroke="url(#gradient2)"
          strokeWidth="2"
          strokeLinecap="round"
          fill="none"
          opacity="0.6"
        />
        <path
          d="M8 12 Q12 6, 16 12 T24 12"
          stroke="url(#gradient2)"
          strokeWidth="2"
          strokeLinecap="round"
          fill="none"
          opacity="0.6"
        />
        
        {/* Center dot - origin point */}
        <circle cx="16" cy="16" r="2.5" fill="url(#gradient3)" />
        
        {/* Gradients */}
        <defs>
          <linearGradient id="gradient1" x1="0" y1="0" x2="32" y2="32">
            <stop offset="0%" stopColor="#6366f1" />
            <stop offset="50%" stopColor="#8b5cf6" />
            <stop offset="100%" stopColor="#6366f1" />
          </linearGradient>
          <linearGradient id="gradient2" x1="0" y1="0" x2="32" y2="0">
            <stop offset="0%" stopColor="#6366f1" stopOpacity="0.3" />
            <stop offset="50%" stopColor="#8b5cf6" />
            <stop offset="100%" stopColor="#6366f1" stopOpacity="0.3" />
          </linearGradient>
          <radialGradient id="gradient3">
            <stop offset="0%" stopColor="#a78bfa" />
            <stop offset="100%" stopColor="#6366f1" />
          </radialGradient>
        </defs>
      </svg>
      
      {showText && (
        <span className="text-lg font-bold text-white tracking-tight">
          kiro-let-go
        </span>
      )}
    </div>
  )
}
