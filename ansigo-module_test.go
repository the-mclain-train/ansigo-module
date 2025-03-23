package ansiblemodule

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewModule(t *testing.T) {
	// Save original stdin
	oldStdin := os.Stdin

	// Create a pipe for stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Failed to create pipe: %v", err)
	}
	os.Stdin = r

	// Write test input in a goroutine
	go func() {
		defer w.Close()
		jsonData := map[string]interface{}{
			"_ansible_check_mode": true,
			"_ansible_debug":      false,
			"name":                "test",
		}
		if err := json.NewEncoder(w).Encode(jsonData); err != nil {
			t.Errorf("Failed to write test input: %v", err)
		}
	}()

	// Test basic module creation
	argSpec := ArgSpecMap{
		"name": ArgumentSpec{
			Type:     "str",
			Required: true,
		},
	}
	module, err := NewModule(argSpec, nil, nil, nil, nil, true)
	if err != nil {
		t.Fatalf("Failed to create module: %v", err)
	}
	if module == nil {
		t.Fatal("Module is nil")
	}

	// Restore original stdin
	os.Stdin = oldStdin

	// Test check mode validation
	_, err = NewModule(argSpec, nil, nil, nil, nil, false)
	if err == nil {
		t.Error("Expected error for unsupported check mode")
	}
}

func TestParseInput(t *testing.T) {
	module := &AnsibleModule{
		ArgSpec: ArgSpecMap{
			"name": ArgumentSpec{
				Type:     "str",
				Required: true,
			},
		},
	}

	// Test invalid JSON input
	err := module.parseInput()
	if err == nil {
		t.Error("Expected error for invalid input")
	}
}

func TestValidateArguments(t *testing.T) {
	module := &AnsibleModule{
		ArgSpec: ArgSpecMap{
			"name": ArgumentSpec{
				Type:     "str",
				Required: true,
			},
			"age": ArgumentSpec{
				Type:     "int",
				Required: false,
				Default:  25,
			},
			"enabled": ArgumentSpec{
				Type:     "bool",
				Required: false,
				Default:  true,
			},
			"tags": ArgumentSpec{
				Type:     "list",
				Required: false,
				Default:  []interface{}{"default"},
			},
			"config": ArgumentSpec{
				Type: "dict",
				Options: ArgSpecMap{
					"host": ArgumentSpec{
						Type:     "str",
						Required: true,
					},
					"port": ArgumentSpec{
						Type:     "int",
						Required: false,
						Default:  8080,
					},
				},
			},
		},
		Params: ModuleParams{
			"name":    "test",
			"age":     30,
			"enabled": true,
			"tags":    []interface{}{"test", "demo"},
			"config": map[string]interface{}{
				"host": "localhost",
				"port": 9090,
			},
		},
	}

	// Test valid arguments
	err := module.validateArguments()
	if err != nil {
		t.Errorf("Validation failed for valid arguments: %v", err)
	}

	// Test missing required argument
	module.Params = ModuleParams{}
	err = module.validateArguments()
	if err == nil {
		t.Error("Expected error for missing required argument")
	}

	// Test invalid type for name
	module.Params = ModuleParams{
		"name": 123, // Should be string
	}
	err = module.validateArguments()
	if err == nil {
		t.Error("Expected error for invalid type")
	}

	// Test invalid type for age
	module.Params = ModuleParams{
		"name": "test",
		"age":  "not a number",
	}
	err = module.validateArguments()
	if err == nil {
		t.Error("Expected error for invalid age type")
	}

	// Test invalid type for enabled
	module.Params = ModuleParams{
		"name":    "test",
		"enabled": "not a boolean",
	}
	err = module.validateArguments()
	if err == nil {
		t.Error("Expected error for invalid enabled type")
	}

	// Test invalid type for tags
	module.Params = ModuleParams{
		"name": "test",
		"tags": 123, // Not a list or string
	}
	err = module.validateArguments()
	if err == nil {
		t.Error("Expected error for invalid tags type")
	}

	// Test invalid nested config
	module.Params = ModuleParams{
		"name": "test",
		"config": map[string]interface{}{
			"port": "not a number",
		},
	}
	err = module.validateArguments()
	if err == nil {
		t.Error("Expected error for invalid nested config")
	}

	// Test missing required nested argument
	module.Params = ModuleParams{
		"name": "test",
		"config": map[string]interface{}{
			"port": 9090,
		},
	}
	err = module.validateArguments()
	if err == nil {
		t.Error("Expected error for missing required nested argument")
	}
}

func TestValidateArgument(t *testing.T) {
	module := &AnsibleModule{
		Params: make(ModuleParams),
	}

	tests := []struct {
		name     string
		value    interface{}
		spec     ArgumentSpec
		expected error
	}{
		{
			name:  "valid string",
			value: "test",
			spec: ArgumentSpec{
				Type: "str",
			},
			expected: nil,
		},
		{
			name:  "invalid string type",
			value: 123,
			spec: ArgumentSpec{
				Type: "str",
			},
			expected: fmt.Errorf("must be a string"),
		},
		{
			name:  "valid boolean",
			value: true,
			spec: ArgumentSpec{
				Type: "bool",
			},
			expected: nil,
		},
		{
			name:  "valid boolean string",
			value: "true",
			spec: ArgumentSpec{
				Type: "bool",
			},
			expected: nil,
		},
		{
			name:  "invalid boolean",
			value: "not a boolean",
			spec: ArgumentSpec{
				Type: "bool",
			},
			expected: fmt.Errorf("must be a boolean"),
		},
		{
			name:  "valid integer",
			value: 123,
			spec: ArgumentSpec{
				Type: "int",
			},
			expected: nil,
		},
		{
			name:  "valid integer string",
			value: "123",
			spec: ArgumentSpec{
				Type: "int",
			},
			expected: nil,
		},
		{
			name:  "invalid integer",
			value: "not a number",
			spec: ArgumentSpec{
				Type: "int",
			},
			expected: fmt.Errorf("must be an integer"),
		},
		{
			name:  "valid float",
			value: 123.45,
			spec: ArgumentSpec{
				Type: "float",
			},
			expected: nil,
		},
		{
			name:  "valid float string",
			value: "123.45",
			spec: ArgumentSpec{
				Type: "float",
			},
			expected: nil,
		},
		{
			name:  "invalid float",
			value: "not a number",
			spec: ArgumentSpec{
				Type: "float",
			},
			expected: fmt.Errorf("must be a float"),
		},
		{
			name:  "valid list",
			value: []interface{}{"item1", "item2"},
			spec: ArgumentSpec{
				Type: "list",
			},
			expected: nil,
		},
		{
			name:  "valid list string",
			value: "item1,item2",
			spec: ArgumentSpec{
				Type: "list",
			},
			expected: nil,
		},
		{
			name:  "invalid list",
			value: 123, // Not a list or string
			spec: ArgumentSpec{
				Type: "list",
			},
			expected: fmt.Errorf("must be a list"),
		},
		{
			name:  "valid dict",
			value: map[string]interface{}{"key": "value"},
			spec: ArgumentSpec{
				Type: "dict",
			},
			expected: nil,
		},
		{
			name:  "invalid dict",
			value: "not a dict",
			spec: ArgumentSpec{
				Type: "dict",
			},
			expected: fmt.Errorf("must be a dictionary/map"),
		},
		{
			name:  "valid path",
			value: "/path/to/file",
			spec: ArgumentSpec{
				Type: "path",
			},
			expected: nil,
		},
		{
			name:  "invalid path",
			value: 123,
			spec: ArgumentSpec{
				Type: "path",
			},
			expected: fmt.Errorf("must be a path string"),
		},
		{
			name:  "valid choice",
			value: "option1",
			spec: ArgumentSpec{
				Type:    "str",
				Choices: []string{"option1", "option2"},
			},
			expected: nil,
		},
		{
			name:  "invalid choice",
			value: "option3",
			spec: ArgumentSpec{
				Type:    "str",
				Choices: []string{"option1", "option2"},
			},
			expected: fmt.Errorf("must be one of"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := module.validateArgument(test.name, test.value, test.spec)
			if test.expected == nil {
				if err != nil {
					t.Errorf("Expected no error, got: %v", err)
				}
			} else {
				if err == nil {
					t.Error("Expected error, got nil")
				} else if !strings.Contains(err.Error(), test.expected.Error()) {
					t.Errorf("Expected error containing '%s', got: %v", test.expected.Error(), err)
				}
			}
		})
	}
}

func TestAddWarningAndDeprecation(t *testing.T) {
	module := &AnsibleModule{}

	// Test AddWarning
	warningMsg := "This is a test warning"
	module.AddWarning(warningMsg)
	if len(module.Warnings) != 1 {
		t.Errorf("Expected 1 warning, got %d", len(module.Warnings))
	}
	if module.Warnings[0] != warningMsg {
		t.Errorf("Expected warning '%s', got '%s'", warningMsg, module.Warnings[0])
	}

	// Test AddDeprecation without version
	deprecationMsg := "This feature is deprecated"
	module.AddDeprecation(deprecationMsg, "")
	if len(module.DeprecationMsgs) != 1 {
		t.Errorf("Expected 1 deprecation message, got %d", len(module.DeprecationMsgs))
	}
	if module.DeprecationMsgs[0] != deprecationMsg {
		t.Errorf("Expected deprecation message '%s', got '%s'", deprecationMsg, module.DeprecationMsgs[0])
	}

	// Test AddDeprecation with version
	versionedMsg := "This feature will be removed"
	version := "2.0.0"
	module.AddDeprecation(versionedMsg, version)
	if len(module.DeprecationMsgs) != 2 {
		t.Errorf("Expected 2 deprecation messages, got %d", len(module.DeprecationMsgs))
	}
	expectedVersionedMsg := fmt.Sprintf("%s (version: %s)", versionedMsg, version)
	if module.DeprecationMsgs[1] != expectedVersionedMsg {
		t.Errorf("Expected versioned deprecation message '%s', got '%s'", expectedVersionedMsg, module.DeprecationMsgs[1])
	}
}

func TestParseBoolean(t *testing.T) {
	module := &AnsibleModule{}

	tests := []struct {
		input    string
		expected bool
		hasError bool
	}{
		{"yes", true, false},
		{"true", true, false},
		{"1", true, false},
		{"no", false, false},
		{"false", false, false},
		{"0", false, false},
		{"invalid", false, true},
	}

	for _, test := range tests {
		result, err := module.parseBoolean(test.input)
		if test.hasError {
			if err == nil {
				t.Errorf("Expected error for input %s", test.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("Unexpected error for input %s: %v", test.input, err)
			continue
		}
		if result != test.expected {
			t.Errorf("Expected %v for input %s, got %v", test.expected, test.input, result)
		}
	}
}

func TestExitJson(t *testing.T) {
	module := &AnsibleModule{
		TestMode: true,
		Params: ModuleParams{
			"test_param": "test_value",
		},
	}
	result := map[string]interface{}{
		"changed": true,
		"msg":     "test",
	}

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Create a channel to receive the output
	output := make(chan string)
	go func() {
		var buf bytes.Buffer
		io.Copy(&buf, r)
		output <- buf.String()
	}()

	// Set up a custom exit function that doesn't actually exit
	module.ExitFunc = func(code int) {
		// In test mode, we don't actually exit
	}

	// Call ExitJson and expect it to panic
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected ExitJson to panic in test mode")
		}
	}()

	module.ExitJson(result)

	// Restore stdout
	w.Close()
	os.Stdout = oldStdout

	// Get the output
	jsonOutput := <-output

	// Verify the output
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(jsonOutput), &parsed); err != nil {
		t.Fatalf("Failed to parse JSON output: %v", err)
	}

	// Check expected fields
	if parsed["changed"] != true {
		t.Error("Expected changed to be true")
	}
	if parsed["msg"] != "test" {
		t.Error("Expected msg to be 'test'")
	}
	if invocation, ok := parsed["invocation"].(map[string]interface{}); ok {
		if invocation["test_param"] != "test_value" {
			t.Error("Expected test_param to be 'test_value'")
		}
	} else {
		t.Error("Expected invocation to be a map")
	}
}

func TestFailJson(t *testing.T) {
	module := &AnsibleModule{
		TestMode: true,
		Params: ModuleParams{
			"test_param": "test_value",
		},
	}
	msg := "test error"
	args := map[string]interface{}{
		"rc": 1,
	}

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Create a channel to receive the output
	output := make(chan string)
	go func() {
		var buf bytes.Buffer
		io.Copy(&buf, r)
		output <- buf.String()
	}()

	// Set up a custom exit function that doesn't actually exit
	module.ExitFunc = func(code int) {
		// In test mode, we don't actually exit
	}

	// Call FailJson and expect it to panic
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected FailJson to panic in test mode")
		}
	}()

	module.FailJson(msg, args)

	// Restore stdout
	w.Close()
	os.Stdout = oldStdout

	// Get the output
	jsonOutput := <-output

	// Verify the output
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(jsonOutput), &parsed); err != nil {
		t.Fatalf("Failed to parse JSON output: %v", err)
	}

	// Check expected fields
	if parsed["failed"] != true {
		t.Error("Expected failed to be true")
	}
	if parsed["msg"] != msg {
		t.Error("Expected msg to match input")
	}
	if parsed["rc"] != float64(1) {
		t.Error("Expected rc to be 1")
	}
	if invocation, ok := parsed["invocation"].(map[string]interface{}); ok {
		if invocation["test_param"] != "test_value" {
			t.Error("Expected test_param to be 'test_value'")
		}
	} else {
		t.Error("Expected invocation to be a map")
	}
}

func TestRunCommand(t *testing.T) {
	module := &AnsibleModule{}

	// Test successful command
	result, err := module.RunCommand("echo", []string{"test"}, nil, "")
	if err != nil {
		t.Fatalf("Command failed: %v", err)
	}
	if result.Stdout != "test\n" {
		t.Errorf("Expected stdout 'test\\n', got '%s'", result.Stdout)
	}

	// Test command with error
	_, err = module.RunCommand("nonexistent", nil, nil, "")
	if err == nil {
		t.Error("Expected error for nonexistent command")
	}
}

func TestGetBinPath(t *testing.T) {
	module := &AnsibleModule{}

	// Test existing binary
	path, err := module.GetBinPath("echo", true)
	if err != nil {
		t.Fatalf("Failed to find echo: %v", err)
	}
	if path == "" {
		t.Error("Expected non-empty path for echo")
	}

	// Test nonexistent binary
	_, err = module.GetBinPath("nonexistent", true)
	if err == nil {
		t.Error("Expected error for nonexistent binary")
	}
}

func TestMD5(t *testing.T) {
	module := &AnsibleModule{}

	// Create test file
	tmpFile, err := os.CreateTemp("", "test-*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	content := "test content"
	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}

	// Test MD5 calculation
	hash, err := module.MD5(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to calculate MD5: %v", err)
	}
	if hash == "" {
		t.Error("Expected non-empty MD5 hash")
	}

	// Test nonexistent file
	_, err = module.MD5("nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent file")
	}
}

func TestAtomicMove(t *testing.T) {
	module := &AnsibleModule{}

	// Create test files
	srcFile, err := os.CreateTemp("", "src-*.txt")
	if err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}
	defer os.Remove(srcFile.Name())

	destFile := filepath.Join(os.TempDir(), "dest.txt")
	defer os.Remove(destFile)

	content := "test content"
	if _, err := srcFile.WriteString(content); err != nil {
		t.Fatalf("Failed to write to source file: %v", err)
	}

	// Test atomic move
	changed, err := module.AtomicMove(srcFile.Name(), destFile)
	if err != nil {
		t.Fatalf("Failed to move file: %v", err)
	}
	if !changed {
		t.Error("Expected file to be changed")
	}

	// Verify destination file
	destContent, err := os.ReadFile(destFile)
	if err != nil {
		t.Fatalf("Failed to read destination file: %v", err)
	}
	if string(destContent) != content {
		t.Error("Destination file content doesn't match source")
	}
}

func TestTmpFile(t *testing.T) {
	module := &AnsibleModule{}

	// Test temporary file creation
	file, err := module.TmpFile("test-")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer file.Close()

	if file == nil {
		t.Error("Expected non-nil file")
	}
}

func TestCleanup(t *testing.T) {
	module := &AnsibleModule{}
	module.TmpDir = os.TempDir() + "/test-tmp"
	os.MkdirAll(module.TmpDir, 0755)

	module.Cleanup()
	if _, err := os.Stat(module.TmpDir); err == nil {
		t.Error("Temporary directory still exists after cleanup")
	}
}

func TestGetParam(t *testing.T) {
	module := &AnsibleModule{
		Params: ModuleParams{
			"string": "test",
			"int":    42,
			"bool":   true,
		},
	}

	tests := []struct {
		name     string
		expected interface{}
	}{
		{"string", "test"},
		{"int", 42},
		{"bool", true},
		{"nonexistent", nil},
	}

	for _, test := range tests {
		result := module.GetParam(test.name)
		if result != test.expected {
			t.Errorf("Expected %v for %s, got %v", test.expected, test.name, result)
		}
	}
}

func TestGetParamBool(t *testing.T) {
	module := &AnsibleModule{
		Params: ModuleParams{
			"true":    true,
			"false":   false,
			"yes":     "yes",
			"no":      "no",
			"invalid": "invalid",
		},
	}

	tests := []struct {
		name     string
		expected bool
		hasError bool
	}{
		{"true", true, false},
		{"false", false, false},
		{"yes", true, false},
		{"no", false, false},
		{"invalid", false, true},
		{"nonexistent", false, true},
	}

	for _, test := range tests {
		result, err := module.GetParamBool(test.name)
		if test.hasError {
			if err == nil {
				t.Errorf("Expected error for %s", test.name)
			}
			continue
		}
		if err != nil {
			t.Errorf("Unexpected error for %s: %v", test.name, err)
			continue
		}
		if result != test.expected {
			t.Errorf("Expected %v for %s, got %v", test.expected, test.name, result)
		}
	}
}

func TestGetParamInt(t *testing.T) {
	module := &AnsibleModule{
		Params: ModuleParams{
			"int":     42,
			"float":   42.0,
			"string":  "42",
			"invalid": "invalid",
		},
	}

	tests := []struct {
		name     string
		expected int
		hasError bool
	}{
		{"int", 42, false},
		{"float", 42, false},
		{"string", 42, false},
		{"invalid", 0, true},
		{"nonexistent", 0, true},
	}

	for _, test := range tests {
		result, err := module.GetParamInt(test.name)
		if test.hasError {
			if err == nil {
				t.Errorf("Expected error for %s", test.name)
			}
			continue
		}
		if err != nil {
			t.Errorf("Unexpected error for %s: %v", test.name, err)
			continue
		}
		if result != test.expected {
			t.Errorf("Expected %v for %s, got %v", test.expected, test.name, result)
		}
	}
}

func TestGetParamString(t *testing.T) {
	module := &AnsibleModule{
		Params: ModuleParams{
			"string": "test",
			"int":    42,
			"bool":   true,
		},
	}

	tests := []struct {
		name     string
		expected string
		hasError bool
	}{
		{"string", "test", false},
		{"int", "42", false},
		{"bool", "true", false},
		{"nonexistent", "", true},
	}

	for _, test := range tests {
		result, err := module.GetParamString(test.name)
		if test.hasError {
			if err == nil {
				t.Errorf("Expected error for %s", test.name)
			}
			continue
		}
		if err != nil {
			t.Errorf("Unexpected error for %s: %v", test.name, err)
			continue
		}
		if result != test.expected {
			t.Errorf("Expected %v for %s, got %v", test.expected, test.name, result)
		}
	}
}

func TestGetParamStringList(t *testing.T) {
	module := &AnsibleModule{
		Params: ModuleParams{
			"list":    []interface{}{"a", "b", "c"},
			"string":  "a,b,c",
			"empty":   "",
			"invalid": 42,
		},
	}

	tests := []struct {
		name     string
		expected []string
		hasError bool
	}{
		{"list", []string{"a", "b", "c"}, false},
		{"string", []string{"a", "b", "c"}, false},
		{"empty", []string{}, false},
		{"invalid", nil, true},
		{"nonexistent", nil, true},
	}

	for _, test := range tests {
		result, err := module.GetParamStringList(test.name)
		if test.hasError {
			if err == nil {
				t.Errorf("Expected error for %s", test.name)
			}
			continue
		}
		if err != nil {
			t.Errorf("Unexpected error for %s: %v", test.name, err)
			continue
		}
		if len(result) != len(test.expected) {
			t.Errorf("Expected length %d for %s, got %d", len(test.expected), test.name, len(result))
			continue
		}
		for i := range result {
			if result[i] != test.expected[i] {
				t.Errorf("Expected %v for %s, got %v", test.expected, test.name, result)
				break
			}
		}
	}
}

func TestCreateDiff(t *testing.T) {
	module := &AnsibleModule{}

	diff := module.CreateDiff("before", "after", "before header", "after header")
	if diff["before"] != "before" {
		t.Error("Expected 'before' in diff")
	}
	if diff["after"] != "after" {
		t.Error("Expected 'after' in diff")
	}
	if diff["before_header"] != "before header" {
		t.Error("Expected 'before header' in diff")
	}
	if diff["after_header"] != "after header" {
		t.Error("Expected 'after header' in diff")
	}
}

func TestFileOperations(t *testing.T) {
	module := &AnsibleModule{}

	// Create test directory and files
	tmpDir, err := os.MkdirTemp("", "test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	testFile := filepath.Join(tmpDir, "test.txt")
	testDir := filepath.Join(tmpDir, "testdir")
	testSymlink := filepath.Join(tmpDir, "symlink")

	// Test FileExists
	if module.FileExists(testFile) {
		t.Error("File should not exist yet")
	}

	// Create test file
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	if !module.FileExists(testFile) {
		t.Error("File should exist now")
	}

	// Test IsDir
	if module.IsDir(testFile) {
		t.Error("File should not be a directory")
	}

	if err := os.MkdirAll(testDir, 0755); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}

	if !module.IsDir(testDir) {
		t.Error("Directory should be a directory")
	}

	// Test IsFile
	if !module.IsFile(testFile) {
		t.Error("File should be a file")
	}

	if module.IsFile(testDir) {
		t.Error("Directory should not be a file")
	}

	// Test IsSymlink
	if err := os.Symlink(testFile, testSymlink); err != nil {
		t.Fatalf("Failed to create symlink: %v", err)
	}

	if !module.IsSymlink(testSymlink) {
		t.Error("Path should be a symlink")
	}

	// Test IsExecutable
	if module.IsExecutable(testFile) {
		t.Error("File should not be executable")
	}

	if err := os.Chmod(testFile, 0755); err != nil {
		t.Fatalf("Failed to make file executable: %v", err)
	}

	if !module.IsExecutable(testFile) {
		t.Error("File should be executable")
	}
}

func TestFileStat(t *testing.T) {
	module := &AnsibleModule{}

	// Create test file
	tmpFile, err := os.CreateTemp("", "test-*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	content := "test content"
	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}

	// Test FileStat
	stat, err := module.FileStat(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to get file stat: %v", err)
	}

	if !stat["exists"].(bool) {
		t.Error("File should exist")
	}
	if stat["path"] != tmpFile.Name() {
		t.Error("Path should match")
	}
	if stat["size"] != int64(len(content)) {
		t.Error("Size should match content length")
	}
	if !stat["isreg"].(bool) {
		t.Error("Should be a regular file")
	}
}

func TestCompareFiles(t *testing.T) {
	module := &AnsibleModule{}

	// Create test files
	tmpFile1, err := os.CreateTemp("", "test1-*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file 1: %v", err)
	}
	defer os.Remove(tmpFile1.Name())

	tmpFile2, err := os.CreateTemp("", "test2-*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file 2: %v", err)
	}
	defer os.Remove(tmpFile2.Name())

	// Test identical files
	content := "test content"
	if _, err := tmpFile1.WriteString(content); err != nil {
		t.Fatalf("Failed to write to temp file 1: %v", err)
	}
	if _, err := tmpFile2.WriteString(content); err != nil {
		t.Fatalf("Failed to write to temp file 2: %v", err)
	}

	identical, err := module.CompareFiles(tmpFile1.Name(), tmpFile2.Name())
	if err != nil {
		t.Fatalf("Failed to compare files: %v", err)
	}
	if !identical {
		t.Error("Files should be identical")
	}

	// Test different files
	if _, err := tmpFile2.WriteString("different"); err != nil {
		t.Fatalf("Failed to write to temp file 2: %v", err)
	}

	identical, err = module.CompareFiles(tmpFile1.Name(), tmpFile2.Name())
	if err != nil {
		t.Fatalf("Failed to compare files: %v", err)
	}
	if identical {
		t.Error("Files should not be identical")
	}
}

func TestCopyFile(t *testing.T) {
	module := &AnsibleModule{}

	// Create test file
	tmpFile, err := os.CreateTemp("", "test-*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	content := "test content"
	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}

	destFile := filepath.Join(os.TempDir(), "dest.txt")
	defer os.Remove(destFile)

	// Test file copy
	changed, err := module.CopyFile(tmpFile.Name(), destFile, 0644)
	if err != nil {
		t.Fatalf("Failed to copy file: %v", err)
	}
	if !changed {
		t.Error("File should be changed")
	}

	// Verify destination file
	destContent, err := os.ReadFile(destFile)
	if err != nil {
		t.Fatalf("Failed to read destination file: %v", err)
	}
	if string(destContent) != content {
		t.Error("Destination file content doesn't match source")
	}
}

func TestCreateDirectory(t *testing.T) {
	module := &AnsibleModule{}

	tmpDir := filepath.Join(os.TempDir(), "testdir")
	defer os.RemoveAll(tmpDir)

	// Test directory creation
	changed, err := module.CreateDirectory(tmpDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create directory: %v", err)
	}
	if !changed {
		t.Error("Directory should be changed")
	}

	// Test existing directory
	changed, err = module.CreateDirectory(tmpDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create existing directory: %v", err)
	}
	if changed {
		t.Error("Directory should not be changed")
	}
}

func TestCreateSymlink(t *testing.T) {
	module := &AnsibleModule{}

	// Create test file
	tmpFile, err := os.CreateTemp("", "test-*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	content := "test content"
	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}

	symlink := filepath.Join(os.TempDir(), "symlink")
	defer os.Remove(symlink)

	// Test symlink creation
	changed, err := module.CreateSymlink(tmpFile.Name(), symlink)
	if err != nil {
		t.Fatalf("Failed to create symlink: %v", err)
	}
	if !changed {
		t.Error("Symlink should be changed")
	}

	// Verify symlink
	target, err := os.Readlink(symlink)
	if err != nil {
		t.Fatalf("Failed to read symlink: %v", err)
	}
	if target != tmpFile.Name() {
		t.Error("Symlink target doesn't match source")
	}
}

func TestReadTextFile(t *testing.T) {
	module := &AnsibleModule{}

	// Create test file
	tmpFile, err := os.CreateTemp("", "test-*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	content := "test content"
	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}

	// Test file reading
	result, err := module.ReadTextFile(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}
	if result != content {
		t.Error("File content doesn't match")
	}
}

func TestWriteTextFile(t *testing.T) {
	module := &AnsibleModule{}

	tmpFile := filepath.Join(os.TempDir(), "test.txt")
	defer os.Remove(tmpFile)

	content := "test content"

	// Test file writing
	changed, err := module.WriteTextFile(tmpFile, content, 0644)
	if err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}
	if !changed {
		t.Error("File should be changed")
	}

	// Verify file content
	result, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}
	if string(result) != content {
		t.Error("File content doesn't match")
	}

	// Test writing same content
	changed, err = module.WriteTextFile(tmpFile, content, 0644)
	if err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}
	if changed {
		t.Error("File should not be changed")
	}
}

func TestRegexReplace(t *testing.T) {
	module := &AnsibleModule{}

	tests := []struct {
		text        string
		pattern     string
		replacement string
		expected    string
		hasError    bool
	}{
		{"hello world", "world", "there", "hello there", false},
		{"hello world", "\\w+", "word", "word word", false},
		{"hello world", "[", "word", "", true},
	}

	for _, test := range tests {
		result, err := module.RegexReplace(test.text, test.pattern, test.replacement)
		if test.hasError {
			if err == nil {
				t.Errorf("Expected error for pattern %s", test.pattern)
			}
			continue
		}
		if err != nil {
			t.Errorf("Unexpected error for pattern %s: %v", test.pattern, err)
			continue
		}
		if result != test.expected {
			t.Errorf("Expected %v for pattern %s, got %v", test.expected, test.pattern, result)
		}
	}
}

func TestHasChanged(t *testing.T) {
	module := &AnsibleModule{}

	result := module.HasChanged(true, nil)
	if !result["changed"].(bool) {
		t.Error("Expected changed to be true")
	}

	result = module.HasChanged(false, map[string]interface{}{"msg": "test"})
	if result["changed"].(bool) {
		t.Error("Expected changed to be false")
	}
	if result["msg"] != "test" {
		t.Error("Expected msg to be preserved")
	}
}

func TestAppendToFile(t *testing.T) {
	module := &AnsibleModule{}

	tmpFile := filepath.Join(os.TempDir(), "test.txt")
	defer os.Remove(tmpFile)

	content := "test content"

	// Test appending to new file
	changed, err := module.AppendToFile(tmpFile, content)
	if err != nil {
		t.Fatalf("Failed to append to file: %v", err)
	}
	if !changed {
		t.Error("File should be changed")
	}

	// Test appending same content
	changed, err = module.AppendToFile(tmpFile, content)
	if err != nil {
		t.Fatalf("Failed to append to file: %v", err)
	}
	if changed {
		t.Error("File should not be changed")
	}

	// Test appending different content
	changed, err = module.AppendToFile(tmpFile, "different content")
	if err != nil {
		t.Fatalf("Failed to append to file: %v", err)
	}
	if !changed {
		t.Error("File should be changed")
	}
}

func TestDebugMsg(t *testing.T) {
	module := &AnsibleModule{
		Debug: true,
	}

	// Note: This is hard to test as it writes to stderr
	// In a real test environment, you might want to capture stderr
	module.DebugMsg("test message")
}

func TestBackupFile(t *testing.T) {
	module := &AnsibleModule{}

	// Create test file
	tmpFile, err := os.CreateTemp("", "test-*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	content := "test content"
	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}

	// Test backup creation
	backupPath, err := module.BackupFile(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to create backup: %v", err)
	}
	defer os.Remove(backupPath)

	// Verify backup file
	backupContent, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("Failed to read backup file: %v", err)
	}
	if string(backupContent) != content {
		t.Error("Backup file content doesn't match source")
	}
}

func TestPreserveSELinuxContext(t *testing.T) {
	module := &AnsibleModule{}

	// This is a placeholder that panics, so we expect it to panic
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic")
		}
	}()

	module.PreserveSELinuxContext("test")
}
