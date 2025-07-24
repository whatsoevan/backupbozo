from pathlib import Path
from PIL import Image, ExifTags
from datetime import datetime
import subprocess
import json
from rich import print
import shutil
from rich.progress import Progress, BarColumn, TextColumn
import os
import sys
from rich.prompt import Prompt, Confirm
from rich.console import Console

console = Console()

# --- Metadata extraction functions (still useful) ---
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

# --- Progress tracking and file processing ---
PROGRESS_FILENAME = "sort_progress.json"

def load_progress(progress_path):
    if progress_path.exists():
        try:
            with open(progress_path, "r") as f:
                return json.load(f)
        except Exception as e:
            print(f"[yellow]Warning: Could not load progress file: {e}[/yellow]")
    return {"copied": {}, "skipped": {}}

def save_progress(progress_path, progress):
    try:
        with open(progress_path, "w") as f:
            json.dump(progress, f, indent=2)
    except Exception as e:
        print(f"[red]Error saving progress: {e}[/red]")

def get_all_files_recursive(directory):
    directory = Path(directory)
    for root, _, files in os.walk(directory):
        for file in files:
            yield Path(root) / file

def get_month_folder(date):
    return date.strftime("%Y-%m")

def write_html_report(dest_dir, copied, skipped, report_path):
    html = [
        "<html><head><title>Photo Sort Report</title></head><body>",
        f"<h1>Photo Sort Report</h1>",
        f"<h2>Copied Files ({len(copied)})</h2>",
        "<ul>"
    ]
    for src, dst in copied.items():
        html.append(f'<li>{src} &rarr; <a href="file://{dst}">{dst}</a></li>')
    html.append("</ul>")
    html.append(f"<h2>Skipped Files ({len(skipped)})</h2>")
    html.append("<ul>")
    for src, reason in skipped.items():
        html.append(f'<li>{src} - {reason}</li>')
    html.append("</ul>")
    html.append("</body></html>")
    with open(report_path, "w") as f:
        f.write("\n".join(html))

def process_files(src_dir, dest_dir, progress_path, report_path):
    progress = load_progress(progress_path)
    copied = progress.get("copied", {})
    skipped = progress.get("skipped", {})

    files = list(get_all_files_recursive(src_dir))
    total_files = len(files)
    if total_files == 0:
        console.print("[red]No files found to process.[/red]")
        return

    # --- Preliminary scan summary ---
    console.print(f"[cyan]Preliminary scan:[/cyan] Found [bold]{total_files}[/bold] files to process.")

    copied_count_before = len(copied)
    skipped_count_before = len(skipped)

    with Progress(
        TextColumn("[progress.description]{task.description}"),
        BarColumn(),
        TextColumn("{task.completed}/{task.total}"),
    ) as progress_bar:
        task = progress_bar.add_task("Processing files...", total=total_files)

        for file_path in files:

            metadata_date, modified_date = get_dates(file_path)
            date = metadata_date or modified_date
            if not date:
                skipped[str(file_path)] = "no_date"
                save_progress(progress_path, {"copied": copied, "skipped": skipped})
                progress_bar.update(task, advance=1)
                continue

            month_folder = get_month_folder(date)
            dest_month_dir = Path(dest_dir) / month_folder
            dest_month_dir.mkdir(parents=True, exist_ok=True)
            dest_file = dest_month_dir / file_path.name

            if dest_file.exists():
                skipped[str(file_path)] = str(dest_file)
                save_progress(progress_path, {"copied": copied, "skipped": skipped})
                progress_bar.update(task, advance=1)
                continue

            try:
                shutil.copy2(file_path, dest_file)
                copied[str(file_path)] = str(dest_file)
            except Exception as e:
                skipped[str(file_path)] = f"copy_error: {e}"
            save_progress(progress_path, {"copied": copied, "skipped": skipped})
            progress_bar.update(task, advance=1)

    # Final save (no folders)
    save_progress(progress_path, {"copied": copied, "skipped": skipped})

    # --- Reporting summary ---
    copied_count = len(copied)
    skipped_count = len(skipped)
    copied_this_run = copied_count - copied_count_before
    skipped_this_run = skipped_count - skipped_count_before
    total_processed = copied_this_run + skipped_this_run
    match = (total_processed == total_files)

    console.rule("[bold green]Summary Report[/bold green]")
    console.print(f"[bold]Total files found:[/bold] {total_files}")
    console.print(f"[green]Copied this run:[/green] {copied_this_run}")
    console.print(f"[yellow]Skipped this run:[/yellow] {skipped_this_run}")
    if match:
        console.print(f"[bold green]✔ All files accounted for![/bold green] ({copied_this_run} + {skipped_this_run} = {total_files})")
    else:
        console.print(f"[bold red]✖ Mismatch![/bold red] Processed: {total_processed}, Expected: {total_files}")
    console.print(f"[dim]Cumulative copied: {copied_count}, cumulative skipped: {skipped_count}[/dim]")
    console.print(f"[blue]Progress file:[/blue] {progress_path}")
    console.print(f"[blue]HTML report:[/blue] {report_path}")
    console.rule()

    # Write HTML report
    write_html_report(dest_dir, copied, skipped, report_path)

# --- Main entry ---
if __name__ == "__main__":
    console.print("[bold cyan]iPhone/Photo Sorter[/bold cyan]")
    console.print("Choose source for photos:")
    source_choice = Prompt.ask("[1] Mount iPhone with ifuse\n[2] Use local folder\nEnter 1 or 2", choices=["1", "2"])

    if source_choice == "1":
        check_ifuse_installed()
        console.print("[yellow]Please connect, unlock, and trust your iPhone, then press Enter to continue.[/yellow]")
        input()
        src_dir = mount_iphone()
        unmount_after = True
    else:
        src_dir = Prompt.ask("Enter the path to the source directory")
        src_dir = Path(src_dir)
        unmount_after = False

    dest_dir = Prompt.ask("Enter the path to the destination directory")
    dest_dir = Path(dest_dir)
    timestamp = datetime.now().strftime("%Y%m%d_%H%M%S")
    progress_path = dest_dir / f"sort_progress_{timestamp}.json"
    report_path = dest_dir / f"sort_report_{timestamp}.html"

    try:
        process_files(src_dir, dest_dir, progress_path, report_path)
    finally:
        if source_choice == "1" and unmount_after:
            unmount_iphone()
