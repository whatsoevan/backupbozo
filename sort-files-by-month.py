from pathlib import Path
from PIL import Image, ExifTags
from datetime import datetime
import subprocess
import json
from rich import print
import shutil
from rich.progress import Progress, BarColumn, TextColumn


def get_exif_date(photo_path):
    try:
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
    try:
        return datetime.fromtimestamp(path.stat().st_mtime)
    except Exception as e:
        print(f"[MODTIME EXCEPTION] {path.name}: {e}")
        return None


def get_dates(file_path):
    ext = file_path.suffix.lower()
    metadata_date = None

    if ext in [".jpg", ".jpeg"]:
        metadata_date = get_exif_date(file_path)
    elif ext in [".mp4", ".mov", ".mkv", ".webm", ".avi"]:
        metadata_date = get_video_creation_date(file_path)

    modified_date = get_modified_date(file_path)
    return metadata_date, modified_date


def scan_directory(directory):
    directory = Path(directory)
    print(f"{'File Name':<25} {'Metadata':<12} {'Modified':<12}")
    print("-" * 60)

    def format_display_string(value, width=15):
        text = value.strftime("%Y-%m-%d") if value else "None"

        padded = f"{text:<{width}}"
        if value:
            return padded
        else:
            return f"[dim]{padded}[/dim]"

    for file in directory.iterdir():
        if file.is_file():
            metadata_date, modified_date = get_dates(file)
            name = f"{file.name:<25}"

            meta_disp = format_display_string(metadata_date, 12)
            mod_disp = format_display_string(modified_date, 12)

            if (
                metadata_date
                and modified_date
                and (
                    metadata_date.year != modified_date.year
                    or metadata_date.month != modified_date.month
                )
            ):
                # TODO: move this to a conflict folder, where you can handsort.
                print(f"[red]{name} {meta_disp} {mod_disp}[/red]")
            elif not metadata_date and modified_date:
                # TODO: move to the month of the modified date.
                print(f"{name} {'No metadata':<15} {mod_disp}")
            else:
                # TODO: copy this to the metadata date
                print(f"{name} {meta_disp} {mod_disp}")


def copy_files_with_progress(src_folder_str, dest_folder_str):
    src_folder = Path(src_folder_str)
    dest_folder = Path(dest_folder_str)

    if not src_folder.is_dir():
        print(f"Error: '{src_folder}' is not a valid directory.")
        return

    if not dest_folder.exists():
        dest_folder.mkdir(parents=True)

    files = [f for f in src_folder.iterdir() if f.is_file()]
    total_files = len(files)

    if total_files == 0:
        print("No files to copy.")
        return

    with Progress(
        TextColumn("[progress.description]{task.description}"),
        BarColumn(),
        TextColumn("{task.completed}/{task.total}"),
    ) as progress:
        task = progress.add_task("Copying files...", total=total_files)

        for file_path in files:
            shutil.copy2(file_path, dest_folder / file_path.name)
            print(f"Copied {file_path.name} to {dest_folder}")
            progress.update(task, advance=1)

    print(f"Copied {total_files} files from '{src_folder}' to '{dest_folder}'")


if __name__ == "__main__":
    import sys

    if len(sys.argv) < 2:
        print("Usage: python sort-files-by-month.py <directory>")
    else:
        scan_directory(sys.argv[1])
