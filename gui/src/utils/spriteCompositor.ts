import * as THREE from 'three';

// Cache for loaded images so we don't fetch them multiple times
const imageCache: Record<string, HTMLImageElement> = {};

async function loadImage(src: string): Promise<HTMLImageElement> {
  if (imageCache[src]) return imageCache[src];
  
  return new Promise((resolve, reject) => {
    const img = new Image();
    img.crossOrigin = 'anonymous';
    img.onload = () => {
      imageCache[src] = img;
      resolve(img);
    };
    img.onerror = reject;
    img.src = src;
  });
}

export interface SpriteLayer {
  src: string;
  tint?: string; // Optional hex color to tint this layer (e.g., for skin color or uniform)
}

/**
 * Composites multiple transparent sprite sheets into a single CanvasTexture.
 */
export async function compositeSpriteSheet(layers: SpriteLayer[]): Promise<THREE.CanvasTexture> {
  // We assume all layers are the same dimensions. We'll size the canvas based on the first loaded layer.
  let width = 0;
  let height = 0;
  
  const loadedImages = await Promise.all(
    layers.map(async (layer) => {
      try {
        const img = await loadImage(layer.src);
        if (width === 0) width = img.width;
        if (height === 0) height = img.height;
        return { img, tint: layer.tint };
      } catch (err) {
        console.warn(`Failed to load sprite layer: ${layer.src}`, err);
        return null;
      }
    })
  );

  // Fallback size if nothing loads
  if (width === 0) width = 256;
  if (height === 0) height = 256;

  const canvas = document.createElement('canvas');
  canvas.width = width;
  canvas.height = height;
  const ctx = canvas.getContext('2d');
  
  if (!ctx) {
    throw new Error("Failed to get 2d context for sprite compositor");
  }

  // Draw layers in order (index 0 is bottom-most, e.g., body)
  for (const layer of loadedImages) {
    if (!layer) continue;
    
    if (layer.tint) {
      // To tint a sprite layer, we draw it to a temporary canvas, apply source-in composite with the color, 
      // then draw the tinted result onto our main canvas.
      const tempCanvas = document.createElement('canvas');
      tempCanvas.width = width;
      tempCanvas.height = height;
      const tCtx = tempCanvas.getContext('2d');
      if (tCtx) {
        tCtx.drawImage(layer.img, 0, 0);
        tCtx.globalCompositeOperation = 'source-in';
        tCtx.fillStyle = layer.tint;
        tCtx.fillRect(0, 0, width, height);
        ctx.drawImage(tempCanvas, 0, 0);
      } else {
        ctx.drawImage(layer.img, 0, 0);
      }
    } else {
      ctx.drawImage(layer.img, 0, 0);
    }
  }

  // Create a Three.js CanvasTexture from our composed canvas
  const texture = new THREE.CanvasTexture(canvas);
  texture.minFilter = THREE.NearestFilter;
  texture.magFilter = THREE.NearestFilter;
  texture.colorSpace = THREE.SRGBColorSpace;
  
  return texture;
}

/**
 * Helper to determine layers based on agent data
 */
export function getAgentSpriteLayers(
  country?: string,
  role?: string,
  gender?: string,
  employmentType?: string
): SpriteLayer[] {
  // 1. BASE BODY
  const bodyLayer: SpriteLayer = { src: '/sprite_body.png' };
  
  // Example dynamic tinting based on country or random factors
  if (country === 'India') bodyLayer.tint = '#c68642'; // warm skin tone
  else if (country === 'Japan') bodyLayer.tint = '#f1c27d'; 
  
  const layers: SpriteLayer[] = [bodyLayer];
  
  // 2. HAIR
  if (gender !== 'none') {
     layers.push({ src: '/sprite_hair_1.png' });
  }

  // 3. CLOTHES
  if (role === 'Office Manager') {
    layers.push({ src: '/sprite_clothes_2.png' }); // Red shirt for managers
  } else if (employmentType === 'consultancy') {
    layers.push({ src: '/sprite_clothes_1.png', tint: '#9ca3af' }); // Grey tinted blue shirt
  } else {
    layers.push({ src: '/sprite_clothes_1.png' }); // Blue shirt
  }
  
  return layers;
}
