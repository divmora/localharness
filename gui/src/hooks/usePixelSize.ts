import { useLayoutEffect, useState } from 'react';

export function usePixelPanelSizes(minPx: number, maxPx: number, defaultPx: number) {
  const [sizes, setSizes] = useState({ minSize: 10, maxSize: 50, defaultSize: 20 });

  useLayoutEffect(() => {
    const handleResize = () => {
      const w = window.innerWidth;
      // Safeguard against tiny windows
      if (w === 0) return;

      const minPercentage = (minPx / w) * 100;
      const maxPercentage = (maxPx / w) * 100;
      const defaultPercentage = (defaultPx / w) * 100;

      setSizes({
        minSize: Math.max(1, Math.min(minPercentage, 90)),
        maxSize: Math.max(10, Math.min(maxPercentage, 99)),
        defaultSize: Math.max(1, Math.min(defaultPercentage, 99)),
      });
    };
    handleResize();
    window.addEventListener('resize', handleResize);
    return () => window.removeEventListener('resize', handleResize);
  }, [minPx, maxPx, defaultPx]);

  return sizes;
}
