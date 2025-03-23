# AnsiGo Module

A Go implementation of Ansible's `module_utils/basic.py` functionality, providing a framework for writing Ansible modules in Go.

## Overview

AnsiGo Module is a Go package that implements the core functionality of Ansible's module utilities, making it possible to write Ansible modules in Go. It provides a familiar interface for Ansible module developers while leveraging Go's performance and type safety.

## Features

- Full compatibility with Ansible's module interface
- JSON input/output handling
- Argument validation and type conversion
- File operations (copy, move, symlink)
- Command execution
- Temporary file management
- Debug and logging support
- Check mode support
- Warning and deprecation message handling

## Installation

```bash
go get github.com/the-mclain-train/ansigo-module
```

## Usage

### Basic Module Structure

```go
package main

import (
    "github.com/the-mclain-train/ansigo-module"
)

func main() {
    // Define argument specifications
    argSpec := ansiblemodule.ArgSpecMap{
        "name": ansiblemodule.ArgumentSpec{
            Type:     "str",
            Required: true,
        },
        "state": ansiblemodule.ArgumentSpec{
            Type:     "str",
            Required: true,
            Choices:  []string{"present", "absent"},
        },
    }

    // Create module instance
    module, err := ansiblemodule.NewModule(argSpec, nil, nil, nil, nil, true)
    if err != nil {
        module.FailJson(err.Error(), nil)
        return
    }
    defer module.Cleanup()

    // Get parameters
    name, err := module.GetParamString("name")
    if err != nil {
        module.FailJson(err.Error(), nil)
        return
    }

    state, err := module.GetParamString("state")
    if err != nil {
        module.FailJson(err.Error(), nil)
        return
    }

    // Module logic here
    changed := false
    result := make(map[string]interface{})

    // Return results
    if err != nil {
        module.FailJson(err.Error(), result)
        return
    }

    module.ExitJson(ansiblemodule.HasChanged(changed, result))
}
```

### File Operations Example

```go
// Copy a file
changed, err := module.CopyFile("source.txt", "dest.txt", 0644)
if err != nil {
    module.FailJson(err.Error(), nil)
    return
}

// Create a directory
changed, err = module.CreateDirectory("/path/to/dir", 0755)
if err != nil {
    module.FailJson(err.Error(), nil)
    return
}

// Create a symlink
changed, err = module.CreateSymlink("/path/to/source", "/path/to/link")
if err != nil {
    module.FailJson(err.Error(), nil)
    return
}
```

### Command Execution Example

```go
// Run a command
result, err := module.RunCommand("ls", []string{"-l"}, nil, "")
if err != nil {
    module.FailJson(err.Error(), nil)
    return
}

// Use command output
if result.Rc != 0 {
    module.FailJson("Command failed", map[string]interface{}{
        "rc":     result.Rc,
        "stdout": result.Stdout,
        "stderr": result.Stderr,
    })
    return
}
```

### File Backup Example

```go
// Create a backup of a file
backupPath, err := module.BackupFile("/path/to/file")
if err != nil {
    module.FailJson(err.Error(), nil)
    return
}
```

## Testing

Run the tests:

```bash
go test -v ./...
```

## Contributing

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

This project is licensed under the MIT License - see the LICENSE file for details.

## Acknowledgments

- Ansible project for the original Python implementation
- Go community for the excellent standard library