# BackupBozo

<div align="center">
  <img src="icon.webp" alt="BackupBozo Mascot" width="200"/>

  *A small friend in a world full of big tech companies trying to farm your personal photos, videos, and every moment in your life.*

  [![Go](https://img.shields.io/badge/Go-1.23+-00ADD8?style=flat&logo=go)](https://golang.org/)
  [![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
</div>

## About

BackupBozo is a purposefully dumb tool. It moves files from one folder to another, where they are organized into YYYY-MM folders. That's it.

I built this project because I wanted an easy way to backup my photos onto my computer; just plug and chug. No messing around with clicking and dragging and organizing into folders by month.

I know there are other tools out there on the internet for this, but I honestly have trust issues uploading my images to anything. Bozo is stupid, on purpose. I genuinely made it that way because I do not want to see any of your personal photos. I wouldn't even want your data even if I somehow knew how to get it. I also just didn't trust downloading other tools because tools like this mess with your hard drive, and I wouldn't want to install malware that steals my data. Thankfully, I wouldn't know how to write malware or steal your data, even if I wanted to (again, I do not want to see your photos. Neither does Bozo). 

## 🚀 Features

- **🔄 Incremental Backups**: Only processes new/changed files since last backup, so you can just plug in your phone and trust Bozo to pick up everything since last time.
- **🎯 Smart Deduplication**: Bozo will catch duplicates! Even old pictures, so you never waste space on the same photo twice.
- **📅 Intelligent Organization**: Automatically sorts files into YYYY-MM folders.
- **⚡ High Performance**: Multi-threaded processing with streaming I/O. Uses as many cores your computer has to speed things up!
- **📊 Detailed Reports**: HTML reports are generated, so you know exactly what happened and don't need to dig through your files.
- **💾 Space-Aware**: Aborts backup if you don't have enough space.

## 📋 Requirements

- **Go 1.23+** for building from source
- **ffprobe** (from FFmpeg) for video metadata extraction:
  - **Ubuntu/Debian**: `sudo apt install ffmpeg`
  - **macOS**: `brew install ffmpeg`
  - **Windows**: Download from [ffmpeg.org](https://ffmpeg.org/download.html)

## 🔧 Installation

### Option 1: Build from Source
```bash
git clone https://github.com/whatsoevan/backupbozo.git
cd backupbozo
go build -o backupbozo .
```

### Option 2: Download Pre-built Binary
Download the latest release from [Releases](https://github.com/whatsoevan/backupbozo/releases) page.

## 🎯 Quick Start

### Interactive Mode (Recommended)
```bash
./backupbozo
```
The interactive mode will guide you through selecting source and destination folders with a user-friendly interface.

### Command Line Mode
```bash
# Basic backup
./backupbozo --src ~/DCIM --dest ~/backup_photos

# Full backup (disable incremental mode)
./backupbozo --src ~/DCIM --dest ~/backup_photos --incremental=false

# Custom database and report locations
./backupbozo --src ~/DCIM --dest ~/backup_photos --db ~/backup.db --report ~/report.html
```

## 📖 How It Works

1. **Planning Phase**: Scans source directory and estimates space requirements
2. **Deduplication**: Checks SHA256 hashes against existing backup database
3. **Organization**: Extracts dates from EXIF data (photos) or metadata (videos)
4. **Backup**: Copies new files to `YYYY-MM/` folders in destination
5. **Reporting**: Generates HTML report with backup summary and file links

### File Organization Example
```
backup_photos/
├── 2024-01/
│   ├── IMG_001.jpg
│   └── VID_001.mp4
├── 2024-02/
│   ├── IMG_002.jpg
│   └── IMG_003.heic
└── reports/
    └── backup_2024-02-15_14-30-25.html
```

## ⚙️ Command Line Options

| Flag | Default | Description |
|------|---------|-------------|
| `--src` | - | Source directory to backup |
| `--dest` | - | Destination backup directory |
| `--db` | `dest/backupbozo.db` | SQLite database location |
| `--report` | `dest/reports/` | HTML report output location |
| `--incremental` | `true` | Enable incremental backup mode |
| `--workers` | CPU cores | Number of parallel processing workers |
| `--batch-size` | `100` | Database batch insert size |

## 🔍 Metadata Support

- **Images**: EXIF date extraction (JPEG, PNG, HEIC, TIFF, etc.)
- **Videos**: ffprobe metadata extraction (MP4, MOV, AVI, MKV, etc.)
- **Fallback**: File modification time when metadata unavailable

## 📊 Performance

BackupBozo was built because I have some really old computers trying to do this stuff. I kept Bozo as lean as possible, but ultimately this depends on your computer.

- **Streaming I/O**: Single-pass hash computation and file copying (50% I/O reduction)
- **Parallel Processing**: Multi-core worker pools for maximum throughput
- **Smart Caching**: In-memory hash cache for O(1) duplicate detection
- **Batch Operations**: Inserts into database in batches, so one query can handle many files.

## 🤝 Contributing

Feel free to submit issues or feature requests. This is a silly side project, but if you find it useful I thank you for using it!

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

---

<div align="center">
  <sub>Made with ❤️ for preserving your digital memories</sub>
</div>