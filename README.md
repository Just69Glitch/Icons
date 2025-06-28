# SVG Icon Server Application

## Overview

This application is an HTTP server that provides a way to serve and list SVG icons from a specified directory. The server is configurable via a `config.json` file, and user inputs are validated for correctness.

### Features
- **Serve SVG Icons**: Hosts an HTTP endpoint for serving and listing `.svg` icons.
- **Configurable**: Allows users to specify a directory for icons and a port number.
- **Dynamic Configuration**: Prompts the user for valid configurations if none are available or if configurations are invalid.
- **Interactive Interface**: Uses the `zenity` package for graphical prompts for inputs and error handling.

## Requirements
- **Go Language**: Version 1.16 or later.
- **Dependencies**: Install `github.com/ncruces/zenity` for graphical input dialogs.
- **Icons Directory**: Must contain `.svg` files.

## Installation

1. Clone the repository or download the code.
2. Install dependencies:
  ```bash
  go get github.com/ncruces/zenity
  ```

3. Build the application:

  ```bash
  go build -o build/SVGIconPackServer.exe
  ```

## Usage

1. **Run the Application**:

   ```bash
   cd build
   ./SVGIconPackServer.exe
   ```
2. **Configure Port**:

   * If no valid port is configured, the application prompts the user for a valid port (1-65535).
3. **Configure Icon Directory**:

   * If no valid directory is found, the application prompts the user to select a directory containing `.svg` icons.
4. **Access the Server**:

   * Open a browser and navigate to `http://localhost:<PORT>/Icons/` to view the icons.

## API Endpoints

### `GET /Icons/list`

Lists `.svg` icons in the configured directory with pagination.

* **Query Parameters**:

  * `page`: Page number (default is 1).
  * `limit`: Number of items per page (default is 1000).
* **Response**:

  ```json
  {
    "page": 1,
    "limit": 1000,
    "total": 25,
    "files": ["icon1.svg", "icon2.svg", ...]
  }
  ```

### `GET /Icons/<filename>`

Serves the requested `.svg` file directly.

## Configuration

The configuration is stored in `config.json`:

```json
{
  "port": "8080",
  "iconDir": "/path/to/icons"
}
```

* If the file doesn't exist, it is created automatically with default values.
* Invalid configurations are automatically corrected via prompts.

## Key Components

### Functions

* `isValidPort(portStr string)`: Validates port numbers.
* `isValidIconDir(path string)`: Checks if the directory contains `.svg` files.
* `promptForValidPort()`: Prompts for a valid port number.
* `selectValidIconDir()`: Prompts for a directory containing `.svg` files.
* `getSortedIconNames(iconDir string)`: Returns sorted `.svg` filenames from the directory.
* `listIcons(iconDir string)`: HTTP handler for listing icons.

### Main Flow

1. Load or create configuration (`loadOrCreateConfig()`).
2. Validate port and directory.
3. Start the HTTP server on the configured port.