from pathlib import Path
from datetime import datetime
import subprocess
import json
import hashlib

def get_exif_date(photo_path):
    """Extract the EXIF DateTime from a photo, if available. Returns a datetime or None."""
    try:
        from PIL import Image, ExifTags
        with Image.open(photo_path) as img:
            exif_data = img.getexif()
            if exif_data:
                for tag_id, value in exif_data.items():
                    tag = ExifTags.TAGS.get(tag_id)
                    if tag == "DateTime":
                        return datetime.strptime(value, "%Y:%m:%d %H:%M:%S")
    except Exception as e:
        print(f"[EXIF EXCEPTION] {photo_path.name}: {e}")
    return None

def get_video_creation_date(video_path):
    """Extract the creation date from video metadata using ffprobe. Returns a datetime or None."""
    cmd = [
        "ffprobe",
        "-v",
        "quiet",
        "-print_format",
        "json",
        "-show_format",
        str(video_path),
    ]
    try:
        result = subprocess.run(cmd, capture_output=True, text=True, check=True)
        data = json.loads(result.stdout)
        tags = data.get("format", {}).get("tags", {})
        creation_time = tags.get("creation_time")
        if creation_time:
            return datetime.fromisoformat(creation_time.replace("Z", "+00:00"))
    except Exception as e:
        print(f"[FFPROBE EXCEPTION] {video_path.name}: {e}")
    return None

def get_modified_date(path):
    """Get the file's last modified date as a datetime object."""
    try:
        return datetime.fromtimestamp(path.stat().st_mtime)
    except Exception as e:
        print(f"[MODTIME EXCEPTION] {path.name}: {e}")
        return None

def get_dates(file_path):
    """Return (metadata_date, modified_date) for a file, using EXIF/video/mtime as appropriate."""
    ext = file_path.suffix.lower()
    metadata_date = None
    if ext in [".jpg", ".jpeg"]:
        metadata_date = get_exif_date(file_path)
    elif ext in [".mp4", ".mov", ".mkv", ".webm", ".avi"]:
        metadata_date = get_video_creation_date(file_path)
    modified_date = get_modified_date(file_path)
    return metadata_date, modified_date

def compute_file_hash(file_path, block_size=65536):
    """Compute the SHA256 hash of a file. Returns the hex digest or None on error."""
    sha256 = hashlib.sha256()
    try:
        with open(file_path, 'rb') as f:
            for block in iter(lambda: f.read(block_size), b''):
                sha256.update(block)
        return sha256.hexdigest()
    except Exception:
        return None

def get_file_stat_info(file_path):
    """Return (size, mtime) for a file."""
    stat = file_path.stat()
    return stat.st_size, stat.st_mtime 