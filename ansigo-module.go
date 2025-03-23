// Package ansiblemodule provides Go implementations of Ansible module_utils/basic.py functionality
package ansiblemodule

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ArgumentSpec defines the specification for a module argument
type ArgumentSpec struct {
	Type         string      `json:"type,omitempty"`
	Required     bool        `json:"required,omitempty"`
	Default      interface{} `json:"default,omitempty"`
	Choices      []string    `json:"choices,omitempty"`
	NoLog        bool        `json:"no_log,omitempty"`
	Aliases      []string    `json:"aliases,omitempty"`
	Elements     string      `json:"elements,omitempty"`
	Options      ArgSpecMap  `json:"options,omitempty"`
	AppliesTo    []string    `json:"applies_to,omitempty"`
	RemoveInFile string      `json:"removed_in_version,omitempty"`
	SubOptions   ArgSpecMap  `json:"suboptions,omitempty"` // For nested list elements
}

// ArgSpecMap is a map of argument names to their specifications
type ArgSpecMap map[string]ArgumentSpec

// ModuleParams represents a map of parameter names to their values
type ModuleParams map[string]interface{}

// AnsibleModule is the core structure for Ansible modules written in Go
type AnsibleModule struct {
	Params            ModuleParams
	ArgSpec           ArgSpecMap
	CheckMode         bool
	Debug             bool
	Warnings          []string
	DeprecationMsgs   []string
	NoLog             []string
	TmpDir            string
	FromFile          string
	MutuallyExclusive [][]string
	RequiredTogether  [][]string
	RequiredOne       [][]string
	RequiredIf        []RequiredIfSpec
	Aliases           map[string]string
	RequiredBy        map[string][]string // Parameters required by other parameters
	TestMode          bool                // Flag to indicate if we're in test mode
	ExitFunc          func(int)           // Custom exit function for testing
}

// RequiredIfSpec defines a conditional requirement for arguments
type RequiredIfSpec struct {
	Key          string
	Value        interface{}
	Requirements []string
}

// Result represents the structured return data for an Ansible module
type Result struct {
	Changed      bool                   `json:"changed"`
	Failed       bool                   `json:"failed,omitempty"`
	Msg          string                 `json:"msg,omitempty"`
	Stdout       string                 `json:"stdout,omitempty"`
	Stderr       string                 `json:"stderr,omitempty"`
	Rc           int                    `json:"rc,omitempty"`
	Invocation   map[string]interface{} `json:"invocation,omitempty"`
	Warnings     []string               `json:"warnings,omitempty"`
	Deprecations []map[string]string    `json:"deprecations,omitempty"`
	Diff         map[string]interface{} `json:"diff,omitempty"`
	Debug        []string               `json:"debug_info,omitempty"`
	Exception    string                 `json:"exception,omitempty"`
}

// CommandResult contains the results of running a command
type CommandResult struct {
	Cmd    string
	Stdout string
	Stderr string
	Rc     int
}

// NewModule creates a new AnsibleModule instance
func NewModule(argSpec ArgSpecMap, mutuallyExclusive [][]string,
	requiredTogether [][]string, requiredOne [][]string,
	requiredIf []RequiredIfSpec, supports_check_mode bool) (*AnsibleModule, error) {

	module := &AnsibleModule{
		ArgSpec:           argSpec,
		Params:            ModuleParams{},
		Warnings:          []string{},
		DeprecationMsgs:   []string{},
		NoLog:             []string{},
		MutuallyExclusive: mutuallyExclusive,
		RequiredTogether:  requiredTogether,
		RequiredOne:       requiredOne,
		RequiredIf:        requiredIf,
		Aliases:           make(map[string]string),
	}

	// Process aliases
	for argName, spec := range argSpec {
		for _, alias := range spec.Aliases {
			module.Aliases[alias] = argName
		}
		if spec.NoLog {
			module.NoLog = append(module.NoLog, argName)
		}
	}

	// Parse input
	if err := module.parseInput(); err != nil {
		return nil, err
	}

	// Validate arguments
	if err := module.validateArguments(); err != nil {
		module.FailJson(err.Error(), nil)
		return nil, err
	}

	// Set up temporary directory
	tmpDir, err := os.MkdirTemp("", "ansible-go-")
	if err != nil {
		module.FailJson(fmt.Sprintf("Failed to create temp dir: %v", err), nil)
		return nil, err
	}
	module.TmpDir = tmpDir

	// Add check mode validation
	if !supports_check_mode && module.CheckMode {
		return nil, fmt.Errorf("check mode is not supported for this module")
	}

	return module, nil
}

// parseInput parses JSON input from stdin
func (m *AnsibleModule) parseInput() error {
	var inputData ModuleParams

	// Check if running from ANSIBLE_MODULE_ARGS environment
	if moduleArgs := os.Getenv("ANSIBLE_MODULE_ARGS"); moduleArgs != "" {
		if err := json.Unmarshal([]byte(moduleArgs), &inputData); err != nil {
			return fmt.Errorf("failed to parse ANSIBLE_MODULE_ARGS: %v", err)
		}
	} else {
		// Read from stdin
		stdin := bufio.NewReader(os.Stdin)
		inputBytes, err := io.ReadAll(stdin)
		if err != nil {
			return fmt.Errorf("failed to read from stdin: %v", err)
		}

		if len(inputBytes) == 0 {
			return fmt.Errorf("empty input, expecting JSON data")
		}

		if err := json.Unmarshal(inputBytes, &inputData); err != nil {
			return fmt.Errorf("failed to parse input JSON: %v", err)
		}
	}

	// Check for check mode
	if checkMode, ok := inputData["_ansible_check_mode"]; ok {
		if checkModeBool, ok := checkMode.(bool); ok {
			m.CheckMode = checkModeBool
		}
	}

	// Check for debug
	if debug, ok := inputData["_ansible_debug"]; ok {
		if debugBool, ok := debug.(bool); ok {
			m.Debug = debugBool
		}
	}

	// Apply parameters
	for key, value := range inputData {
		// Skip internal Ansible params (starting with _ansible_)
		if !strings.HasPrefix(key, "_ansible_") {
			m.Params[key] = value
		}
	}

	// Apply default values for missing parameters
	for argName, spec := range m.ArgSpec {
		if _, exists := m.Params[argName]; !exists {
			if spec.Default != nil {
				m.Params[argName] = spec.Default
			}
		}
	}

	// Process aliases
	for alias, realName := range m.Aliases {
		if value, exists := m.Params[alias]; exists {
			if _, mainExists := m.Params[realName]; !mainExists {
				m.Params[realName] = value
			}
			// Remove the alias from params to avoid confusion
			delete(m.Params, alias)
		}
	}

	return nil
}

// validateArguments validates all arguments against their specs
func (m *AnsibleModule) validateArguments() error {
	// Check required arguments
	for argName, spec := range m.ArgSpec {
		if spec.Required {
			if _, exists := m.Params[argName]; !exists {
				return fmt.Errorf("missing required argument: %s", argName)
			}
		}

		// Validate argument that was provided
		if value, exists := m.Params[argName]; exists {
			if err := m.validateArgument(argName, value, spec); err != nil {
				return err
			}
		}
	}

	// Check mutually exclusive groups
	for _, group := range m.MutuallyExclusive {
		count := 0
		for _, argName := range group {
			if _, exists := m.Params[argName]; exists {
				count++
			}
		}
		if count > 1 {
			return fmt.Errorf("parameters are mutually exclusive: %s", strings.Join(group, ", "))
		}
	}

	// Check required together groups
	for _, group := range m.RequiredTogether {
		var foundOne, foundAll bool
		foundOne = false
		foundAll = true

		for _, argName := range group {
			if _, exists := m.Params[argName]; exists {
				foundOne = true
			} else {
				foundAll = false
			}
		}

		if foundOne && !foundAll {
			return fmt.Errorf("parameters must be specified together: %s", strings.Join(group, ", "))
		}
	}

	// Check required one of groups
	for _, group := range m.RequiredOne {
		found := false
		for _, argName := range group {
			if _, exists := m.Params[argName]; exists {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("one of the following is required: %s", strings.Join(group, ", "))
		}
	}

	// Check required if conditions
	for _, condition := range m.RequiredIf {
		if value, exists := m.Params[condition.Key]; exists {
			if reflect.DeepEqual(value, condition.Value) {
				for _, requiredArg := range condition.Requirements {
					if _, exists := m.Params[requiredArg]; !exists {
						return fmt.Errorf("%s is required when %s=%v", requiredArg, condition.Key, condition.Value)
					}
				}
			}
		}
	}

	return nil
}

// validateArgument validates a single argument against its spec
func (m *AnsibleModule) validateArgument(name string, value interface{}, spec ArgumentSpec) error {
	// Type validation
	if spec.Type != "" {
		switch spec.Type {
		case "str", "string":
			if _, ok := value.(string); !ok {
				return fmt.Errorf("%s must be a string", name)
			}
		case "bool", "boolean":
			// Convert string representations to bool if needed
			if strVal, ok := value.(string); ok {
				boolVal, err := m.parseBoolean(strVal)
				if err != nil {
					return fmt.Errorf("%s must be a boolean: %v", name, err)
				}
				m.Params[name] = boolVal
			} else if _, ok := value.(bool); !ok {
				return fmt.Errorf("%s must be a boolean", name)
			}
		case "int", "integer":
			// Convert string representations to int if needed
			if strVal, ok := value.(string); ok {
				intVal, err := strconv.Atoi(strVal)
				if err != nil {
					return fmt.Errorf("%s must be an integer: %v", name, err)
				}
				m.Params[name] = intVal
			} else if _, ok := value.(int); !ok {
				// Try to convert from float if it's a whole number
				if floatVal, ok := value.(float64); ok {
					if floatVal == float64(int(floatVal)) {
						m.Params[name] = int(floatVal)
					} else {
						return fmt.Errorf("%s must be an integer", name)
					}
				} else {
					return fmt.Errorf("%s must be an integer", name)
				}
			}
		case "float":
			// Convert string representations to float if needed
			if strVal, ok := value.(string); ok {
				floatVal, err := strconv.ParseFloat(strVal, 64)
				if err != nil {
					return fmt.Errorf("%s must be a float: %v", name, err)
				}
				m.Params[name] = floatVal
			} else if _, ok := value.(float64); !ok {
				// Try to convert from int
				if intVal, ok := value.(int); ok {
					m.Params[name] = float64(intVal)
				} else {
					return fmt.Errorf("%s must be a float", name)
				}
			}
		case "list", "array":
			// Verify it's a list/array
			if _, ok := value.([]interface{}); !ok {
				// Try to convert from comma-separated string
				if strVal, ok := value.(string); ok {
					if strVal == "" {
						m.Params[name] = []interface{}{}
					} else {
						items := strings.Split(strVal, ",")
						itemsInterface := make([]interface{}, len(items))
						for i, item := range items {
							itemsInterface[i] = strings.TrimSpace(item)
						}
						m.Params[name] = itemsInterface
					}
				} else {
					return fmt.Errorf("%s must be a list", name)
				}
			}
		case "dict", "map":
			if _, ok := value.(map[string]interface{}); !ok {
				return fmt.Errorf("%s must be a dictionary/map", name)
			}
		case "path":
			if _, ok := value.(string); !ok {
				return fmt.Errorf("%s must be a path string", name)
			}
		}
	}

	// Choices validation
	if len(spec.Choices) > 0 {
		validChoice := false
		strValue := fmt.Sprintf("%v", value)
		for _, choice := range spec.Choices {
			if choice == strValue {
				validChoice = true
				break
			}
		}
		if !validChoice {
			return fmt.Errorf("%s must be one of: %s", name, strings.Join(spec.Choices, ", "))
		}
	}

	// If this is a nested data structure with options, validate each element
	if spec.Type == "dict" && len(spec.Options) > 0 {
		if dictVal, ok := value.(map[string]interface{}); ok {
			for subArgName, subArgSpec := range spec.Options {
				if subValue, exists := dictVal[subArgName]; exists {
					if err := m.validateArgument(name+"."+subArgName, subValue, subArgSpec); err != nil {
						return err
					}
				} else if subArgSpec.Required {
					return fmt.Errorf("%s.%s is required", name, subArgName)
				}
			}
		}
	}

	// If this is a list with element type, validate each element
	if spec.Type == "list" && spec.Elements != "" {
		if listVal, ok := value.([]interface{}); ok {
			elementSpec := ArgumentSpec{Type: spec.Elements}
			for i, element := range listVal {
				if err := m.validateArgument(fmt.Sprintf("%s[%d]", name, i), element, elementSpec); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// parseBoolean converts various string representations to boolean
func (m *AnsibleModule) parseBoolean(value string) (bool, error) {
	value = strings.ToLower(strings.TrimSpace(value))

	switch value {
	case "yes", "true", "1", "y", "on":
		return true, nil
	case "no", "false", "0", "n", "off":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean value: %s", value)
	}
}

// ExitJson formats and outputs successful JSON result
func (m *AnsibleModule) ExitJson(result map[string]interface{}) {
	// Add invocation data
	invocation := make(map[string]interface{})
	for k, v := range m.Params {
		if m.shouldLog(k) {
			invocation[k] = v
		} else {
			invocation[k] = "VALUE_SPECIFIED_IN_NO_LOG_PARAMETER"
		}
	}
	result["invocation"] = invocation

	// Add warnings if any
	if len(m.Warnings) > 0 {
		result["warnings"] = m.Warnings
	}

	// Add deprecation messages if any
	if len(m.DeprecationMsgs) > 0 {
		deprecations := make([]map[string]string, len(m.DeprecationMsgs))
		for i, msg := range m.DeprecationMsgs {
			deprecations[i] = map[string]string{"msg": msg}
		}
		result["deprecations"] = deprecations
	}

	// Output JSON and exit
	output, err := json.Marshal(result)
	if err != nil {
		// If JSON marshaling fails, fall back to a simple message
		fmt.Fprintf(os.Stderr, "Failed to serialize JSON result: %v\n", err)
		if m.TestMode {
			panic(fmt.Sprintf("Failed to serialize JSON result: %v", err))
		}
		if m.ExitFunc != nil {
			m.ExitFunc(1)
		} else {
			os.Exit(1)
		}
	}

	fmt.Println(string(output))
	if m.TestMode {
		panic("ExitJson called in test mode")
	}
	if m.ExitFunc != nil {
		m.ExitFunc(0)
	} else {
		os.Exit(0)
	}
}

// FailJson formats and outputs failure JSON result
func (m *AnsibleModule) FailJson(msg string, args map[string]interface{}) {
	result := make(map[string]interface{})
	result["failed"] = true
	result["msg"] = msg

	// Add additional args if provided
	maps.Copy(result, args)

	m.ExitJson(result)
}

// AddWarning adds a warning message
func (m *AnsibleModule) AddWarning(warning string) {
	m.Warnings = append(m.Warnings, warning)
}

// AddDeprecation adds a deprecation warning
func (m *AnsibleModule) AddDeprecation(msg string, version string) {
	if version != "" {
		msg = fmt.Sprintf("%s (version: %s)", msg, version)
	}
	m.DeprecationMsgs = append(m.DeprecationMsgs, msg)
}

// shouldLog checks if a parameter should be logged or hidden
func (m *AnsibleModule) shouldLog(param string) bool {
	for _, noLogParam := range m.NoLog {
		if param == noLogParam {
			return false
		}
	}
	return true
}

// RunCommand executes a command and returns the result
func (m *AnsibleModule) RunCommand(cmd string, args []string, environ map[string]string, data string) (CommandResult, error) {
	result := CommandResult{
		Cmd: cmd,
	}

	// Create command
	command := exec.Command(cmd, args...)

	// Set up environment
	if environ != nil {
		env := os.Environ()
		for k, v := range environ {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
		command.Env = env
	}

	// Set up pipes
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	// Provide input if specified
	if data != "" {
		stdin, err := command.StdinPipe()
		if err != nil {
			return result, fmt.Errorf("failed to create stdin pipe: %v", err)
		}
		go func() {
			defer stdin.Close()
			io.WriteString(stdin, data)
		}()
	}

	// Run command
	err := command.Run()

	// Capture output
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()

	// Get exit code
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			if status, ok := exitError.Sys().(syscall.WaitStatus); ok {
				result.Rc = status.ExitStatus()
			} else {
				result.Rc = 1
			}
		} else {
			result.Rc = 1
		}
		return result, fmt.Errorf("command failed: %v", err)
	}

	result.Rc = 0
	return result, nil
}

// GetBinPath locates an executable in the system path
func (m *AnsibleModule) GetBinPath(name string, required bool) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		if required {
			return "", fmt.Errorf("failed to find required executable %s: %v", name, err)
		}
		return "", nil
	}
	return path, nil
}

// MD5 calculates the MD5 hash of a file
func (m *AnsibleModule) MD5(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	hashBytes := hash.Sum(nil)
	return fmt.Sprintf("%x", hashBytes), nil
}

// AtomicMove performs an atomic file operation
func (m *AnsibleModule) AtomicMove(src, dest string) (bool, error) {
	// Check if destination exists and get stats
	destExists := false
	destStat, err := os.Stat(dest)
	if err == nil {
		destExists = true
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("failed to stat destination %s: %v", dest, err)
	}

	// Get source stats
	srcStat, err := os.Stat(src)
	if err != nil {
		return false, fmt.Errorf("failed to stat source %s: %v", src, err)
	}

	// Check if files are the same
	if destExists {
		// Compare sizes
		if destStat.Size() == srcStat.Size() {
			// Compare content with MD5
			srcMD5, err := m.MD5(src)
			if err != nil {
				return false, err
			}

			destMD5, err := m.MD5(dest)
			if err != nil {
				return false, err
			}

			if srcMD5 == destMD5 {
				// Files are identical, no need to move
				return false, nil
			}
		}
	}

	// Perform atomic move
	if err := os.Rename(src, dest); err != nil {
		// Try copy + remove if rename fails (e.g., across devices)
		srcFile, err := os.Open(src)
		if err != nil {
			return false, err
		}
		defer srcFile.Close()

		destFile, err := os.Create(dest)
		if err != nil {
			return false, err
		}
		defer destFile.Close()

		if _, err := io.Copy(destFile, srcFile); err != nil {
			os.Remove(dest) // Clean up partial file
			return false, err
		}

		// Set permissions to match source
		if err := os.Chmod(dest, srcStat.Mode()); err != nil {
			return false, err
		}

		// Remove source
		if err := os.Remove(src); err != nil {
			return false, err
		}
	}

	return true, nil
}

// TmpFile creates a temporary file
func (m *AnsibleModule) TmpFile(prefix string) (*os.File, error) {
	// Ensure tmp dir exists
	if m.TmpDir == "" {
		var err error
		m.TmpDir, err = os.MkdirTemp("", "ansible-go-")
		if err != nil {
			return nil, fmt.Errorf("failed to create temp dir: %v", err)
		}
	}

	return os.CreateTemp(m.TmpDir, prefix)
}

// Cleanup removes temporary files
func (m *AnsibleModule) Cleanup() {
	if m.TmpDir != "" {
		os.RemoveAll(m.TmpDir)
	}
}

// GetParam retrieves a parameter with type conversion
func (m *AnsibleModule) GetParam(name string) interface{} {
	return m.Params[name]
}

// GetParamBool retrieves a boolean parameter
func (m *AnsibleModule) GetParamBool(name string) (bool, error) {
	value, exists := m.Params[name]
	if !exists {
		return false, fmt.Errorf("parameter %s not found", name)
	}

	switch v := value.(type) {
	case bool:
		return v, nil
	case string:
		return m.parseBoolean(v)
	default:
		return false, fmt.Errorf("parameter %s is not a boolean", name)
	}
}

// GetParamInt retrieves an integer parameter
func (m *AnsibleModule) GetParamInt(name string) (int, error) {
	value, exists := m.Params[name]
	if !exists {
		return 0, fmt.Errorf("parameter %s not found", name)
	}

	switch v := value.(type) {
	case int:
		return v, nil
	case float64:
		return int(v), nil
	case string:
		return strconv.Atoi(v)
	default:
		return 0, fmt.Errorf("parameter %s is not an integer", name)
	}
}

// GetParamString retrieves a string parameter
func (m *AnsibleModule) GetParamString(name string) (string, error) {
	value, exists := m.Params[name]
	if !exists {
		return "", fmt.Errorf("parameter %s not found", name)
	}

	return fmt.Sprintf("%v", value), nil
}

// GetParamStringList retrieves a string list parameter
func (m *AnsibleModule) GetParamStringList(name string) ([]string, error) {
	value, exists := m.Params[name]
	if !exists {
		return nil, fmt.Errorf("parameter %s not found", name)
	}

	switch v := value.(type) {
	case []interface{}:
		result := make([]string, len(v))
		for i, item := range v {
			result[i] = fmt.Sprintf("%v", item)
		}
		return result, nil
	case []string:
		return v, nil
	case string:
		if v == "" {
			return []string{}, nil
		}
		return strings.Split(v, ","), nil
	default:
		return nil, fmt.Errorf("parameter %s is not a list", name)
	}
}

// CreateDiff creates a diff structure for reporting changes
func (m *AnsibleModule) CreateDiff(before, after string, beforeHeader, afterHeader string) map[string]interface{} {
	diff := make(map[string]interface{})

	if beforeHeader == "" {
		beforeHeader = "before"
	}
	if afterHeader == "" {
		afterHeader = "after"
	}

	diff["before"] = before
	diff["after"] = after
	diff["before_header"] = beforeHeader
	diff["after_header"] = afterHeader

	return diff
}

// FileExists checks if a file exists
func (m *AnsibleModule) FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// IsDir checks if a path is a directory
func (m *AnsibleModule) IsDir(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// IsFile checks if a path is a regular file
func (m *AnsibleModule) IsFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// IsSymlink checks if a path is a symbolic link
func (m *AnsibleModule) IsSymlink(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeSymlink != 0
}

// IsExecutable checks if a file is executable
func (m *AnsibleModule) IsExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return (info.Mode() & 0111) != 0
}

// FileStat gets detailed file information
func (m *AnsibleModule) FileStat(path string) (map[string]interface{}, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}

	result := make(map[string]interface{})
	result["exists"] = true
	result["path"] = path
	result["mode"] = fmt.Sprintf("%o", info.Mode().Perm())
	result["size"] = info.Size()
	result["isdir"] = info.IsDir()
	result["isreg"] = info.Mode().IsRegular()
	result["islnk"] = info.Mode()&os.ModeSymlink != 0

	// Get link target if it's a symlink
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err == nil {
			result["lnk_target"] = target
		}
	}

	// Get file modification time
	result["mtime"] = info.ModTime().Unix()

	return result, nil
}

// CompareFiles compares the content of two files
func (m *AnsibleModule) CompareFiles(src, dest string) (bool, error) {
	// Check if both files exist
	if !m.FileExists(src) {
		return false, fmt.Errorf("source file %s does not exist", src)
	}
	if !m.FileExists(dest) {
		return false, nil
	}

	// Get stats for both files
	srcStat, err := os.Stat(src)
	if err != nil {
		return false, err
	}
	destStat, err := os.Stat(dest)
	if err != nil {
		return false, err
	}

	// Quick size comparison
	if srcStat.Size() != destStat.Size() {
		return false, nil
	}

	// Compare MD5 sums
	srcMD5, err := m.MD5(src)
	if err != nil {
		return false, err
	}

	destMD5, err := m.MD5(dest)
	if err != nil {
		return false, err
	}

	return srcMD5 == destMD5, nil
}

// CopyFile copies a file with optional mode and ownership
func (m *AnsibleModule) CopyFile(src, dest string, mode os.FileMode) (bool, error) {
	// Check if source exists
	if !m.FileExists(src) {
		return false, fmt.Errorf("source file %s does not exist", src)
	}

	// Check if files are already identical
	if m.FileExists(dest) {
		identical, err := m.CompareFiles(src, dest)
		if err != nil {
			return false, err
		}
		if identical {
			// Files are identical, no need to copy
			return false, nil
		}
	}

	// Create temporary file for atomic operation
	tmpFile, err := m.TmpFile("ansible-copy-")
	if err != nil {
		return false, err
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()

	// Copy content to temporary file
	srcFile, err := os.Open(src)
	if err != nil {
		return false, err
	}
	defer srcFile.Close()

	tmpFile, err = os.Create(tmpPath)
	if err != nil {
		return false, err
	}

	if _, err := io.Copy(tmpFile, srcFile); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return false, err
	}
	tmpFile.Close()

	// Set mode if provided
	if mode != 0 {
		if err := os.Chmod(tmpPath, mode); err != nil {
			os.Remove(tmpPath)
			return false, err
		}
	} else {
		// Use source file mode
		srcInfo, err := os.Stat(src)
		if err != nil {
			os.Remove(tmpPath)
			return false, err
		}
		if err := os.Chmod(tmpPath, srcInfo.Mode().Perm()); err != nil {
			os.Remove(tmpPath)
			return false, err
		}
	}

	// Move temporary file to destination
	changed, err := m.AtomicMove(tmpPath, dest)
	if err != nil {
		os.Remove(tmpPath) // Clean up temp file if move failed
		return false, err
	}

	return changed, nil
}

// CreateDirectory creates a directory with given mode
func (m *AnsibleModule) CreateDirectory(path string, mode os.FileMode) (bool, error) {
	// Check if directory already exists
	if m.IsDir(path) {
		// Directory exists, check mode
		stat, err := os.Stat(path)
		if err != nil {
			return false, err
		}

		if stat.Mode().Perm() == mode {
			// Mode is already correct
			return false, nil
		}

		// Update mode
		if err := os.Chmod(path, mode); err != nil {
			return false, err
		}

		return true, nil
	}

	// Create directory with specified mode
	if err := os.MkdirAll(path, mode); err != nil {
		return false, err
	}

	return true, nil
}

// CreateSymlink creates a symbolic link
func (m *AnsibleModule) CreateSymlink(src, dest string) (bool, error) {
	// Check if destination already exists
	if m.FileExists(dest) {
		// If it's a symlink, check the target
		if m.IsSymlink(dest) {
			target, err := os.Readlink(dest)
			if err != nil {
				return false, err
			}

			if target == src {
				// Symlink already points to the right target
				return false, nil
			}

			// Remove existing symlink
			if err := os.Remove(dest); err != nil {
				return false, err
			}
		} else {
			// Destination exists but is not a symlink
			return false, fmt.Errorf("destination %s exists and is not a symlink", dest)
		}
	}

	// Create parent directory if needed
	dirPath := filepath.Dir(dest)
	if !m.IsDir(dirPath) {
		if err := os.MkdirAll(dirPath, 0755); err != nil {
			return false, err
		}
	}

	// Create symlink
	if err := os.Symlink(src, dest); err != nil {
		return false, err
	}

	return true, nil
}

// ReadTextFile reads a file into a string
func (m *AnsibleModule) ReadTextFile(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// WriteTextFile writes text to a file
func (m *AnsibleModule) WriteTextFile(path, content string, mode os.FileMode) (bool, error) {
	// Check if file exists with same content
	if m.FileExists(path) {
		existingContent, err := m.ReadTextFile(path)
		if err != nil {
			return false, err
		}

		if existingContent == content {
			// Check if mode needs updating
			stat, err := os.Stat(path)
			if err != nil {
				return false, err
			}

			if stat.Mode().Perm() != mode {
				// Update mode
				if err := os.Chmod(path, mode); err != nil {
					return false, err
				}
				return true, nil
			}

			// Content and mode are the same
			return false, nil
		}
	}

	// Create temporary file
	tmpFile, err := m.TmpFile("ansible-write-")
	if err != nil {
		return false, err
	}
	tmpPath := tmpFile.Name()

	// Write content to temporary file
	if _, err := tmpFile.WriteString(content); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return false, err
	}
	tmpFile.Close()

	// Set mode
	if err := os.Chmod(tmpPath, mode); err != nil {
		os.Remove(tmpPath)
		return false, err
	}

	// Move temporary file to destination
	changed, err := m.AtomicMove(tmpPath, path)
	if err != nil {
		os.Remove(tmpPath)
		return false, err
	}

	return changed, nil
}

// RegexReplace performs regex replacement on a string
func (m *AnsibleModule) RegexReplace(text, pattern, replacement string) (string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", err
	}

	return re.ReplaceAllString(text, replacement), nil
}

// HasChanged returns a boolean indicating if something changed
func (m *AnsibleModule) HasChanged(changed bool, result map[string]interface{}) map[string]interface{} {
	if result == nil {
		result = make(map[string]interface{})
	}
	result["changed"] = changed
	return result
}

// AppendToFile appends content to a file
func (m *AnsibleModule) AppendToFile(path, content string) (bool, error) {
	// If file doesn't exist, write content directly
	if !m.FileExists(path) {
		return m.WriteTextFile(path, content, 0644)
	}

	// Read existing content
	existingContent, err := m.ReadTextFile(path)
	if err != nil {
		return false, err
	}

	// Check if content already exists in file
	if strings.Contains(existingContent, content) {
		return false, nil
	}

	// Append content
	newContent := existingContent
	if !strings.HasSuffix(newContent, "\n") {
		newContent += "\n"
	}
	newContent += content

	// Get current file mode
	stat, err := os.Stat(path)
	if err != nil {
		return false, err
	}

	// Write updated content
	return m.WriteTextFile(path, newContent, stat.Mode().Perm())
}

// DebugMsg prints debug information if debug mode is enabled
func (m *AnsibleModule) DebugMsg(msg string) {
	if m.Debug {
		fmt.Fprintf(os.Stderr, "DEBUG: %s\n", msg)
	}
}

// BackupFile creates a backup of a file
func (m *AnsibleModule) BackupFile(path string) (string, error) {
	timestamp := time.Now().Format("2006-01-02-15-04-05")
	backupPath := path + "." + timestamp

	_, err := m.CopyFile(path, backupPath, 0)
	if err != nil {
		return "", err
	}

	return backupPath, nil
}

// PreserveSELinuxContext is a placeholder for preserving SELinux context
func (m *AnsibleModule) PreserveSELinuxContext(path string) error {
	// TODO impement as needed
	panic("not implemented")
	//return nil
}
