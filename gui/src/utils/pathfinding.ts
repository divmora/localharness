interface Node {
  x: number;
  z: number;
  g: number;
  h: number;
  f: number;
  parent: Node | null;
}

export function findPath(
  startX: number,
  startZ: number,
  endX: number,
  endZ: number,
  isWalkable: (x: number, z: number) => boolean,
  maxIterations = 2000
): [number, number][] {
  // Round to nearest integer grid
  startX = Math.round(startX);
  startZ = Math.round(startZ);
  endX = Math.round(endX);
  endZ = Math.round(endZ);

  if (startX === endX && startZ === endZ) return [];

  const openList: Node[] = [];
  const closedSet: Set<string> = new Set();
  
  const startNode: Node = { x: startX, z: startZ, g: 0, h: 0, f: 0, parent: null };
  openList.push(startNode);
  
  let iterations = 0;
  
  while (openList.length > 0 && iterations < maxIterations) {
    iterations++;
    
    // Get node with lowest f
    let lowestIndex = 0;
    for (let i = 1; i < openList.length; i++) {
      if (openList[i].f < openList[lowestIndex].f) {
        lowestIndex = i;
      }
    }
    
    const current = openList[lowestIndex];
    
    // Found goal
    if (current.x === endX && current.z === endZ) {
      const path: [number, number][] = [];
      let curr: Node | null = current;
      while (curr) {
        // Exclude the starting node from the path so we don't try to walk to where we already are
        if (curr.parent !== null) {
          path.push([curr.x, curr.z]);
        }
        curr = curr.parent;
      }
      return path.reverse();
    }
    
    openList.splice(lowestIndex, 1);
    closedSet.add(`${current.x},${current.z}`);
    
    // Generate neighbors
    const neighbors = [
      { x: current.x, z: current.z - 1 },
      { x: current.x, z: current.z + 1 },
      { x: current.x - 1, z: current.z },
      { x: current.x + 1, z: current.z },
    ];
    
    for (const neighbor of neighbors) {
      const neighborKey = `${neighbor.x},${neighbor.z}`;
      if (closedSet.has(neighborKey)) continue;
      
      // If we are evaluating the final destination, we can step on it even if it's "solid" 
      // (since desks are solid but we want to sit at them).
      const isDestination = neighbor.x === endX && neighbor.z === endZ;
      if (!isDestination && !isWalkable(neighbor.x, neighbor.z)) continue;
      
      const g = current.g + 1;
      const h = Math.abs(neighbor.x - endX) + Math.abs(neighbor.z - endZ);
      const f = g + h;
      
      // Check if already in open list with better G
      const existing = openList.find(n => n.x === neighbor.x && n.z === neighbor.z);
      if (existing && g >= existing.g) continue;
      
      if (existing) {
        existing.g = g;
        existing.f = f;
        existing.parent = current;
      } else {
        openList.push({ x: neighbor.x, z: neighbor.z, g, h, f, parent: current });
      }
    }
  }
  
  // No path found or max iterations reached, fallback to direct line
  return [[endX, endZ]];
}
