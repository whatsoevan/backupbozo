# Release Guide

## Creating a Release

1. **Ensure code is ready:**
   ```bash
   go build -o backupbozo .
   ./backupbozo --help  # Test basic functionality
   ```

2. **Create and push a version tag:**
   ```bash
   git tag v1.0.0
   git push origin v1.0.0
   ```

3. **GitHub Actions will automatically:**
   - Build binaries for all platforms (Linux, macOS, Windows - both x64 and ARM64)
   - Generate SHA256 checksums
   - Create a GitHub release with all binaries
   - Include installation and verification instructions

## Release Artifacts

Each release includes:
- `backupbozo-linux-amd64` - Linux x64 binary
- `backupbozo-linux-arm64` - Linux ARM64 binary
- `backupbozo-darwin-amd64` - macOS Intel binary
- `backupbozo-darwin-arm64` - macOS Apple Silicon binary
- `backupbozo-windows-amd64.exe` - Windows x64 binary
- `backupbozo-windows-arm64.exe` - Windows ARM64 binary
- `checksums.txt` - SHA256 checksums for all binaries

## Version Naming

Use semantic versioning (semver):
- `v1.0.0` - Major releases
- `v1.1.0` - Minor releases (new features)
- `v1.0.1` - Patch releases (bug fixes)
- `v1.0.0-beta.1` - Pre-releases

## Testing Before Release

```bash
# Build and test locally first
go build -o backupbozo .

# Test basic commands
./backupbozo --help
./backupbozo --version  # If you add version flag

# Test with sample data
mkdir test_source test_dest
cp some_photos/* test_source/
./backupbozo --src test_source --dest test_dest
```