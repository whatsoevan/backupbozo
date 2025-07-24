import sqlite3
from pathlib import Path
from datetime import datetime

def init_db(db_path):
    """Initialize the SQLite database and create the files table if it doesn't exist."""
    conn = sqlite3.connect(db_path)
    c = conn.cursor()
    c.execute('''CREATE TABLE IF NOT EXISTS files (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        src_path TEXT,
        dest_path TEXT,
        hash TEXT,
        size INTEGER,
        mtime REAL,
        copied_at TIMESTAMP,
        UNIQUE(hash)
    )''')
    c.execute('CREATE INDEX IF NOT EXISTS idx_hash ON files(hash)')
    c.execute('CREATE INDEX IF NOT EXISTS idx_copied_at ON files(copied_at)')
    c.execute('CREATE INDEX IF NOT EXISTS idx_src_size_mtime ON files(src_path, size, mtime)')
    conn.commit()
    return conn

def get_hash_from_db(conn, src_path, size, mtime):
    """Return the hash for a file with the given src_path, size, and mtime, or None if not found."""
    c = conn.cursor()
    c.execute('SELECT hash FROM files WHERE src_path=? AND size=? AND mtime=?', (str(src_path), size, mtime))
    row = c.fetchone()
    return row[0] if row else None

def file_already_processed(conn, file_hash):
    """Return True if a file with the given hash is already in the DB."""
    c = conn.cursor()
    c.execute('SELECT id FROM files WHERE hash = ?', (file_hash,))
    return c.fetchone() is not None

def insert_file_record(conn, src_path, dest_path, file_hash, size, mtime, copied_at):
    """Insert a record for a copied file into the DB."""
    c = conn.cursor()
    c.execute('''INSERT OR IGNORE INTO files (src_path, dest_path, hash, size, mtime, copied_at)
                 VALUES (?, ?, ?, ?, ?, ?)''', (str(src_path), str(dest_path), file_hash, size, mtime, copied_at))
    conn.commit()

def get_report_data(conn):
    """Return a list of (src_path, dest_path) for all copied files."""
    c = conn.cursor()
    c.execute('SELECT src_path, dest_path FROM files')
    return c.fetchall()

def get_last_backup_time_from_db(db_path):
    """Return the datetime of the most recent backup, or None if not found."""
    if not Path(db_path).exists():
        return None
    try:
        conn = sqlite3.connect(db_path)
        c = conn.cursor()
        c.execute("SELECT MAX(copied_at) FROM files WHERE copied_at IS NOT NULL")
        result = c.fetchone()[0]
        conn.close()
        if result:
            return datetime.fromisoformat(result)
    except Exception:
        return None
    return None 