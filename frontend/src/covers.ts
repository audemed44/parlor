// Covers are shrunk in the browser before they're uploaded: a phone photo
// of a box is megabytes, and tiles are a few hundred pixels wide.
const WIDTH = 720;

// shrink redraws an image at most WIDTH pixels wide, as a JPEG. Small pixel
// art (a 240×160 screenshot) is scaled up by whole steps, kept sharp.
export async function shrink(image: Blob): Promise<Blob> {
  const bitmap = await createImageBitmap(image);
  let scale = Math.min(1, WIDTH / bitmap.width);
  const pixelArt = bitmap.width <= 256;
  if (pixelArt) scale = Math.floor(WIDTH / bitmap.width);
  const canvas = document.createElement("canvas");
  canvas.width = Math.round(bitmap.width * scale);
  canvas.height = Math.round(bitmap.height * scale);
  const ctx = canvas.getContext("2d")!;
  ctx.imageSmoothingEnabled = !pixelArt;
  ctx.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
  bitmap.close();
  return new Promise((resolve, reject) =>
    canvas.toBlob(
      (b) => (b ? resolve(b) : reject(new Error("Couldn't read that image"))),
      "image/jpeg",
      0.88,
    ),
  );
}

export const coverURL = (g: { id: number; cover: string }) =>
  g.cover ? `/api/games/${g.id}/cover?v=${g.cover}` : "";
