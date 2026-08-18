/**
 * RingBuffer<T>
 *
 * A fixed-capacity circular buffer. When the buffer is full, the oldest item
 * is silently overwritten by the newest push. Designed to back the breadcrumb
 * trail in the SigTrap SDK (max 50 items per the API contract).
 *
 *  push A  →  [A]
 *  push B  →  [A, B]
 *  push C  →  [A, B, C]  (full, capacity 3)
 *  push D  →  [B, C, D]  (A evicted)
 */
export class RingBuffer<T> {
  private readonly maxSize: number;
  private readonly items: (T | undefined)[];
  private head: number = 0;
  private count: number = 0;

  constructor(maxSize: number) {
    if (maxSize <= 0) {
      throw new RangeError(`RingBuffer maxSize must be > 0, got ${maxSize}`);
    }
    this.maxSize = maxSize;
    this.items = new Array(maxSize);
  }

  /**
   * Push a new item into the buffer.
   * If the buffer is at capacity, the oldest item is overwritten.
   */
  push(item: T): void {
    this.items[this.head % this.maxSize] = item;
    this.head = (this.head + 1) % this.maxSize;
    if (this.count < this.maxSize) {
      this.count++;
    }
  }

  /**
   * Returns all stored items in insertion order (oldest first).
   */
  toArray(): T[] {
    if (this.count === 0) return [];

    const result: T[] = [];
    const startIndex = this.count < this.maxSize ? 0 : this.head;

    for (let i = 0; i < this.count; i++) {
      const idx = (startIndex + i) % this.maxSize;
      result.push(this.items[idx] as T);
    }
    return result;
  }

  /** Number of items currently stored. */
  get size(): number {
    return this.count;
  }

  /** True if the buffer is at max capacity. */
  get isFull(): boolean {
    return this.count === this.maxSize;
  }

  /** Clears all items from the buffer. */
  clear(): void {
    this.head = 0;
    this.count = 0;
    this.items.fill(undefined);
  }
}
