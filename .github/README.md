# CI/CD Workflows

Simple, focused workflows for the GoMCP SDK.

## Available Workflows

### 🔄 **CI Workflow** (`ci.yml`)
**Trigger:** Push to main, Pull requests
**Purpose:** Test SDK across platforms and Go versions

**Features:**
- Tests on Linux, macOS, Windows
- Go versions 1.23 and 1.24
- Format checking and linting
- Race condition detection

### 🚀 **Release Workflow** (`release.yml`)
**Trigger:** Git tags (v*)
**Purpose:** Create GitHub releases

**Features:**
- Automatic changelog generation  
- Tests before release
- GitHub release creation
- Go module versioning

## Workflow Philosophy

This SDK uses a **simplified approach**:

1. **CI** - Tests everything on every push/PR
2. **Release** - Creates GitHub releases on git tags

No complex packaging, Docker builds, or artifact management - just clean, simple SDK releases.

## Usage

### Creating a Release

1. Ensure all tests pass on main branch
2. Create and push a git tag:
   ```bash
   git tag v1.0.0
   git push origin v1.0.0
   ```
3. The release workflow will automatically:
   - Run tests
   - Generate changelog
   - Create GitHub release

### Manual Testing

Run the CI workflow manually from the Actions tab if needed.

## Benefits of Simplification

- ✅ **Faster CI** - No unnecessary builds
- ✅ **Easier maintenance** - Fewer moving parts  
- ✅ **SDK-focused** - Just versioning and testing
- ✅ **Less complexity** - Two workflows instead of five
- ✅ **Clear purpose** - Each workflow has one job