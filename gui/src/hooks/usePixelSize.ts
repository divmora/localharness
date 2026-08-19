import { useLayoutEffect, useState } from 'react';

export function usePixelPanelSizes(minPx: number, maxPx: number, defaultPercentage: number = 20) {
  const [sizes, setSizes] = useState({ minSize: defaultPercentage, maxSize: 50 });

  useLayoutEffect(() => {
    const handleResize = () => {
      const w = window.innerWidth;
      // Safeguard against tiny windows
      if (w === 0) return;
      
      const minPercentage = (minPx / w) * 100;
      const maxPercentage = (maxPx / w) * 100;
      
      setSizes({
        minSize: Math.max(1, Math.min(minPercentage, 90)),
        maxSize: Math.max(10, Math.min(maxPercentage, 99)),
      });
    };
    handleResize();
    window.addEventListener('resize', handleResize);
    return () => window.removeEventListener('resize', handleResize);
  }, [minPx, maxPx]);

  return sizes;
}
