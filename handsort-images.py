import cv2
import numpy as np
import shutil
from pathlib import Path

KEY_TO_MONTH = {
    ord("1"): "January",
    ord("2"): "February",
    ord("3"): "March",
    ord("4"): "April",
    ord("5"): "May",
    ord("6"): "June",
    ord("7"): "July",
    ord("8"): "August",
    ord("9"): "September",
    ord("0"): "October",
    ord("-"): "November",
    ord("="): "December",
}

IMAGE_EXTS = [".jpg", ".jpeg", ".png", ".bmp", ".gif"]
VIDEO_EXTS = [".mp4", ".avi", ".mov", ".mkv"]

MAX_WIDTH = 800
MAX_HEIGHT = 600


def resize_to_fit(img, max_width=MAX_WIDTH, max_height=MAX_HEIGHT):
    h, w = img.shape[:2]
    scale = min(max_width / w, max_height / h, 1.0)
    new_w = int(w * scale)
    new_h = int(h * scale)
    return cv2.resize(img, (new_w, new_h), interpolation=cv2.INTER_AREA)


def center_image_on_canvas(img, canvas_width=MAX_WIDTH, canvas_height=MAX_HEIGHT):
    canvas = np.zeros((canvas_height, canvas_width, 3), dtype=np.uint8)
    h, w = img.shape[:2]
    x_offset = (canvas_width - w) // 2
    y_offset = (canvas_height - h) // 2
    canvas[y_offset : y_offset + h, x_offset : x_offset + w] = img
    return canvas


def load_media(path: Path):
    ext = path.suffix.lower()
    if ext in IMAGE_EXTS:
        img = cv2.imread(str(path))
        return img
    elif ext in VIDEO_EXTS:
        cap = cv2.VideoCapture(str(path))
        ret, frame = cap.read()
        cap.release()
        if ret:
            return frame
    return None


def image_sorter(source_dir, target_dir):
    source_dir = Path(source_dir)
    target_dir = Path(target_dir)
    # Filter only images/videos
    media_files = [
        f for f in source_dir.iterdir() if f.suffix.lower() in IMAGE_EXTS + VIDEO_EXTS
    ]

    for idx, media_path in enumerate(media_files, 1):
        frame = load_media(media_path)
        if frame is None:
            print(f"Failed to load {media_path.name}, skipping.")
            continue

        resized_img = resize_to_fit(frame)
        canvas_img = center_image_on_canvas(resized_img)

        cv2.imshow("Image Sorter", canvas_img)
        print(f"Showing {idx}/{len(media_files)}: {media_path.name}")
        key = cv2.waitKey(0) & 0xFF

        if key == 27:  # ESC to exit
            print("Exiting...")
            break
        elif key == 8:  # Backspace to skip
            print(f"Skipped {media_path.name}")
            continue
        elif key in KEY_TO_MONTH:
            month = KEY_TO_MONTH[key]
            target_folder = target_dir / month
            target_folder.mkdir(parents=True, exist_ok=True)
            dest = target_folder / media_path.name
            if dest.exists():
                print(
                    f"File '{dest.name}' already exists in {target_folder}. Skipping copy."
                )
            else:
                shutil.copy2(media_path, dest)
                print(f"Copied {media_path.name} to {target_folder}")
        else:
            print(
                f"Invalid key pressed ({chr(key) if 32 <= key < 127 else key}), skipping file."
            )

    cv2.destroyAllWindows()


if __name__ == "__main__":
    import sys

    if len(sys.argv) < 3:
        print("Usage: python image_sorter.py <source_directory> <target_directory>")
    else:
        image_sorter(sys.argv[1], sys.argv[2])

