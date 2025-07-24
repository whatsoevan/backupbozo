from pathlib import Path
from datetime import datetime
from rich.prompt import Prompt
from rich.console import Console
from rich.progress import Progress, BarColumn, TextColumn
import shutil
import os
from media_utils import get_dates, compute_file_hash, get_file_stat_info
from db_utils import (
    init_db, get_hash_from_db, file_already_processed, insert_file_record,
    get_report_data, get_last_backup_time_from_db
)

def write_html_report_sqlite(dest_dir, conn, report_path, errors):
    """Generate an HTML report of copied files and errors. Uses absolute file links, no thumbnails. Source and destination are both links."""
    copied = get_report_data(conn)
    css = '''
    <style>
    body { font-family: Arial, sans-serif; margin: 2em; }
    h1 { color: #2c3e50; }
    ul { list-style: none; padding: 0; }
    li.file-entry { margin-bottom: 1em; }
    .file-info { display: inline; }
    .error { color: #c0392b; }
    </style>
    '''
    html = [
        f"<html><head><title>bozobackup report</title>{css}</head><body>",
        f"<h1>BOZOBACKUP</h1>",
        f"<h2>Copied Files ({len(copied)})</h2>",
        "<ul>"
    ]
    for src, dst in copied:
        abs_src = str(Path(src).resolve())
        abs_dst = str(Path(dst).resolve())
        html.append(f'<li class="file-entry"><span class="file-info"><a href="file://{abs_src}">{abs_src}</a> &rarr; <a href="file://{abs_dst}">{abs_dst}</a></span></li>')
    html.append("</ul>")
    if errors:
        html.append(f"<h2>Errors ({len(errors)})</h2>")
        html.append("<ul>")
        for src, err in errors:
            dest_link = None
            if 'dest_file' in err:
                dest_link = err['dest_file']
            elif isinstance(err, tuple) and len(err) == 2 and os.path.exists(err[0]):
                dest_link = err[0]
            abs_src = str(Path(src).resolve())
            if dest_link and os.path.exists(dest_link):
                abs_dest = str(Path(dest_link).resolve())
                html.append(f'<li class="file-entry error"><span class="file-info"><a href="file://{abs_src}">{abs_src}</a> &rarr; <a href="file://{abs_dest}">{abs_dest}</a> - {err}</span></li>')
            else:
                html.append(f'<li class="file-entry error"><a href="file://{abs_src}">{abs_src}</a> - {err}</li>')
        html.append("</ul>")
    html.append("</body></html>")
    with open(report_path, "w") as f:
        f.write("\n".join(html))

def get_all_files_recursive(directory, min_mtime=None):
    """Yield all files recursively in a directory. If min_mtime is set, only yield files with mtime > min_mtime."""
    directory = Path(directory)
    for root, _, files in os.walk(directory):
        for file in files:
            file_path = Path(root) / file
            if min_mtime is not None:
                try:
                    if file_path.stat().st_mtime <= min_mtime:
                        continue
                except Exception:
                    continue
            yield file_path

def get_month_folder(date):
    """Return the YYYY-MM folder name for a given date."""
    return date.strftime("%Y-%m")

def process_files_sqlite(src_dir, dest_dir, db_path, report_path):
    """Process files: copy, hash, and record in the database. Show progress and summary. Optimized to only process files newer than the last backup."""
    conn = init_db(db_path)
    c = conn.cursor()
    c.execute('SELECT MAX(copied_at) FROM files WHERE copied_at IS NOT NULL')
    result = c.fetchone()[0]
    last_backup_time = datetime.fromisoformat(result) if result else None
    min_mtime = last_backup_time.timestamp() if last_backup_time else None

    files = list(get_all_files_recursive(src_dir, min_mtime=min_mtime))
    total_files = len(files)
    if total_files == 0:
        print("[red]No files found to process.[/red]")
        return

    if last_backup_time:
        elapsed = datetime.now() - last_backup_time
        days = elapsed.days
        hours, rem = divmod(elapsed.seconds, 3600)
        minutes, _ = divmod(rem, 60)
        elapsed_str = f"{days} days, {hours} hours, {minutes} minutes ago" if days or hours or minutes else "just now"
        print(f"[green]Last backup was {elapsed_str} (from {db_path})[/green]")
    else:
        print("[yellow]No previous backup found. This will be your first backup![/yellow]")

    print(f"[cyan]Preliminary scan:[/cyan] Found [bold]{total_files}[/bold] files to process.")
    copied_this_run = 0
    duplicates_this_run = 0
    errors = []

    with Progress(
        TextColumn("[progress.description]{task.description}"),
        BarColumn(),
        TextColumn("{task.completed}/{task.total}"),
    ) as progress_bar:
        task = progress_bar.add_task("Processing files...", total=total_files)

        for file_path in files:
            size, mtime = get_file_stat_info(file_path)
            metadata_date, modified_date = get_dates(file_path)
            date = metadata_date or modified_date
            if not date:
                progress_bar.update(task, advance=1)
                continue

            file_hash = get_hash_from_db(conn, file_path, size, mtime)
            if not file_hash:
                file_hash = compute_file_hash(file_path)

            if file_already_processed(conn, file_hash):
                duplicates_this_run += 1
                progress_bar.update(task, advance=1)
                continue

            month_folder = get_month_folder(date)
            dest_month_dir = Path(dest_dir) / month_folder
            dest_month_dir.mkdir(parents=True, exist_ok=True)
            dest_file = dest_month_dir / file_path.name

            if dest_file.exists():
                progress_bar.update(task, advance=1)
                continue

            try:
                shutil.copy2(file_path, dest_file)
                insert_file_record(conn, file_path, str(dest_file), file_hash, size, mtime, datetime.now().isoformat())
                copied_this_run += 1
            except Exception as e:
                errors.append((str(file_path), f"copy_error: {e}"))
            progress_bar.update(task, advance=1)

    c.execute('SELECT COUNT(*) FROM files')
    copied_count = c.fetchone()[0]
    total_processed = copied_this_run + duplicates_this_run + len(errors)
    match = (total_processed == total_files)

    print("\n" + "─"*46 + " Summary Report " + "─"*46)
    print(f"Total files found: {total_files}")
    print(f"Copied this run: {copied_this_run}")
    print(f"Duplicates this run: {duplicates_this_run}")
    print(f"Errors this run: {len(errors)}")
    if match:
        print(f"✔ All files accounted for! ({copied_this_run} + {duplicates_this_run} + {len(errors)} = {total_files})")
    else:
        print(f"✖ Mismatch! Processed: {total_processed}, Expected: {total_files}")
    print(f"Cumulative copied: {copied_count}")
    if last_backup_time:
        elapsed = datetime.now() - last_backup_time
        days = elapsed.days
        hours, rem = divmod(elapsed.seconds, 3600)
        minutes, _ = divmod(rem, 60)
        elapsed_str = f"{days} days, {hours} hours, {minutes} minutes ago" if days or hours or minutes else "just now"
        print(f"Last backup was {elapsed_str} (from {db_path})")
    else:
        print("No previous backup found. This was your first backup!")
    print(f"Database file: {db_path}")
    print(f"HTML report: {report_path}")

    write_html_report_sqlite(dest_dir, conn, report_path, errors)
    conn.close()

def main():
    """Main entry point for the photo backup and sorting script. Optionally, only files with mtime newer than the last backup are processed for efficiency."""
    console = Console()
    dest_dir = Prompt.ask("Enter the path to the destination directory (for backup history check)")
    dest_dir = Path(dest_dir)
    db_path = dest_dir / "backup.db"
    last_time = get_last_backup_time_from_db(db_path)
    if last_time:
        elapsed = datetime.now() - last_time
        days = elapsed.days
        hours, rem = divmod(elapsed.seconds, 3600)
        minutes, _ = divmod(rem, 60)
        elapsed_str = f"{days} days, {hours} hours, {minutes} minutes ago" if days or hours or minutes else "just now"
        console.print(f"[green]Last backup was {elapsed_str} (from {db_path})[/green]")
    else:
        console.print("[yellow]No previous backup found. This will be your first backup![/yellow]")

    # Ask user if they want to only process files newer than last backup
    use_mtime_opt = True
    if last_time:
        use_mtime_opt = Prompt.ask(
            "Only process files newer than the last backup? (recommended)",
            choices=["y", "n"],
            default="y"
        ) == "y"

    console.print("Choose source for photos:")
    source_choice = Prompt.ask("[1] Mount iPhone with ifuse\n[2] Use local folder\nEnter 1 or 2", choices=["1", "2"])

    if source_choice == "1":
        from media_utils import check_ifuse_installed, mount_iphone, unmount_iphone
        check_ifuse_installed()
        console.print("[yellow]Please connect, unlock, and trust your iPhone, then press Enter to continue.[/yellow]")
        input()
        src_dir = mount_iphone()
        unmount_after = True
    else:
        src_dir = Prompt.ask("Enter the path to the source directory")
        src_dir = Path(src_dir)
        unmount_after = False

    report_path = dest_dir / f"sort_report_{datetime.now().strftime('%Y%m%d_%H%M%S')}.html"

    def get_min_mtime():
        if use_mtime_opt and last_time:
            return last_time.timestamp()
        return None

    def process_files_sqlite_with_opt(src_dir, dest_dir, db_path, report_path, console):
        """Process files, optionally only those newer than the last backup. Uses rich for all output."""
        conn = init_db(db_path)
        c = conn.cursor()
        c.execute('SELECT MAX(copied_at) FROM files WHERE copied_at IS NOT NULL')
        result = c.fetchone()[0]
        last_backup_time = datetime.fromisoformat(result) if result else None
        min_mtime = get_min_mtime()

        files = list(get_all_files_recursive(src_dir, min_mtime=min_mtime))
        total_files = len(files)
        if total_files == 0:
            console.print("[red]No files found to process.[/red]")
            return

        if last_backup_time:
            elapsed = datetime.now() - last_backup_time
            days = elapsed.days
            hours, rem = divmod(elapsed.seconds, 3600)
            minutes, _ = divmod(rem, 60)
            elapsed_str = f"{days} days, {hours} hours, {minutes} minutes ago" if days or hours or minutes else "just now"
            console.print(f"[green]Last backup was {elapsed_str} (from {db_path})[/green]")
        else:
            console.print("[yellow]No previous backup found. This will be your first backup![/yellow]")

        console.print(f"[cyan]Preliminary scan:[/cyan] Found [bold]{total_files}[/bold] files to process.")
        copied_this_run = 0
        duplicates_this_run = 0
        errors = []

        with Progress(
            TextColumn("[progress.description]{task.description}"),
            BarColumn(),
            TextColumn("{task.completed}/{task.total}"),
            console=console
        ) as progress_bar:
            task = progress_bar.add_task("Processing files...", total=total_files)

            for file_path in files:
                size, mtime = get_file_stat_info(file_path)
                metadata_date, modified_date = get_dates(file_path)
                date = metadata_date or modified_date
                if not date:
                    progress_bar.update(task, advance=1)
                    continue

                file_hash = get_hash_from_db(conn, file_path, size, mtime)
                if not file_hash:
                    file_hash = compute_file_hash(file_path)

                if file_already_processed(conn, file_hash):
                    duplicates_this_run += 1
                    progress_bar.update(task, advance=1)
                    continue

                month_folder = get_month_folder(date)
                dest_month_dir = Path(dest_dir) / month_folder
                dest_month_dir.mkdir(parents=True, exist_ok=True)
                dest_file = dest_month_dir / file_path.name

                if dest_file.exists():
                    progress_bar.update(task, advance=1)
                    continue

                try:
                    shutil.copy2(file_path, dest_file)
                    insert_file_record(conn, file_path, str(dest_file), file_hash, size, mtime, datetime.now().isoformat())
                    copied_this_run += 1
                except Exception as e:
                    errors.append((str(file_path), f"copy_error: {e}"))
                progress_bar.update(task, advance=1)

        c.execute('SELECT COUNT(*) FROM files')
        copied_count = c.fetchone()[0]
        total_processed = copied_this_run + duplicates_this_run + len(errors)
        match = (total_processed == total_files)

        console.rule("[bold green]Summary Report[/bold green]")
        console.print(f"[bold]Total files found:[/bold] {total_files}")
        console.print(f"[green]Copied this run:[/green] {copied_this_run}")
        console.print(f"[yellow]Duplicates this run:[/yellow] {duplicates_this_run}")
        console.print(f"[red]Errors this run:[/red] {len(errors)}")
        if match:
            console.print(f"[bold green]✔ All files accounted for![/bold green] ({copied_this_run} + {duplicates_this_run} + {len(errors)} = {total_files})")
        else:
            console.print(f"[bold red]✖ Mismatch![/bold red] Processed: {total_processed}, Expected: {total_files}")
        console.print(f"[dim]Cumulative copied: {copied_count}[/dim]")
        if last_backup_time:
            elapsed = datetime.now() - last_backup_time
            days = elapsed.days
            hours, rem = divmod(elapsed.seconds, 3600)
            minutes, _ = divmod(rem, 60)
            elapsed_str = f"{days} days, {hours} hours, {minutes} minutes ago" if days or hours or minutes else "just now"
            console.print(f"[green]Last backup was {elapsed_str} (from {db_path})[/green]")
        else:
            console.print("[yellow]No previous backup found. This was your first backup!")
        console.print(f"[blue]Database file:[/blue] {db_path}")
        console.print(f"[blue]HTML report:[/blue] [link=file://{report_path.resolve()}]{report_path.resolve()}[/link]")
        console.rule()

        write_html_report_sqlite(dest_dir, conn, report_path, errors)
        conn.close()

    try:
        process_files_sqlite_with_opt(src_dir, dest_dir, db_path, report_path, console)
    finally:
        if source_choice == "1" and unmount_after:
            from media_utils import unmount_iphone
            unmount_iphone()

if __name__ == "__main__":
    main() 