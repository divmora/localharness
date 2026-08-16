interface SkeletonProps {
  className?: string;
  variant?: 'text' | 'circular' | 'rectangular';
  width?: string | number;
  height?: string | number;
  animation?: 'pulse' | 'wave' | 'none';
}

export function Skeleton({
  className = '',
  variant = 'rectangular',
  width,
  height,
  animation = 'pulse',
}: SkeletonProps) {
  const baseClasses = 'bg-bg-tertiary';

  const variantClasses = {
    text: 'rounded h-4',
    circular: 'rounded-full',
    rectangular: 'rounded-md',
  };

  const animationClasses = {
    pulse: 'animate-pulse',
    wave: 'animate-shimmer',
    none: '',
  };

  const style: React.CSSProperties = {};
  if (width) style.width = typeof width === 'number' ? `${width}px` : width;
  if (height) style.height = typeof height === 'number' ? `${height}px` : height;

  const allClasses = `${baseClasses} ${variantClasses[variant]} ${animationClasses[animation]} ${className}`.trim();

  return (
    <div
      className={allClasses}
      style={style}
    />
  );
}

interface SkeletonTextProps {
  lines?: number;
  className?: string;
}

export function SkeletonText({ lines = 3, className = '' }: SkeletonTextProps) {
  return (
    <div className={`flex flex-col gap-2 ${className}`.trim()}>
      {Array.from({ length: lines }).map((_, i) => (
        <Skeleton
          key={i}
          variant="text"
          width={i === lines - 1 ? '60%' : '100%'}
        />
      ))}
    </div>
  );
}

interface SkeletonCardProps {
  className?: string;
  showAvatar?: boolean;
  lines?: number;
}

export function SkeletonCard({ className = '', showAvatar = false, lines = 3 }: SkeletonCardProps) {
  return (
    <div className={`p-4 border border-border-primary rounded-lg bg-bg-secondary ${className}`.trim()}>
      {showAvatar && (
        <div className="flex items-center gap-3 mb-3">
          <Skeleton variant="circular" width={40} height={40} />
          <div className="flex-1">
            <Skeleton variant="text" width="60%" />
          </div>
        </div>
      )}
      <SkeletonText lines={lines} />
    </div>
  );
}