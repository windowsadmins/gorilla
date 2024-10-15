package main

import (
	"crypto/sha256"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"bytes"
	"strings"
	"gopkg.in/yaml.v3"
	"github.com/AlecAivazis/survey/v2"
	"github.com/rodchristiansen/gorilla/pkg/pkginfo"
	"github.com/rodchristiansen/gorilla/pkg/logging"
)

// Installer struct holds the metadata for the installation package, 
// including its location, hash, type, and any additional arguments.
type Installer struct {
	Location  string   `yaml:"location"`
	Hash      string   `yaml:"hash"`
	Arguments []string `yaml:"arguments,omitempty"`
	Type      string   `yaml:"type"`
}

// PkgsInfo struct represents the package information, including 
// metadata such as name, version, developer, and installation scripts.
type PkgsInfo struct {
	Name                  string     `yaml:"name"`
	DisplayName           string     `yaml:"display_name"`
	Version               string     `yaml:"version"`
	Description           string     `yaml:"description"`
	Catalogs              []string   `yaml:"catalogs"`
	Category              string     `yaml:"category"`
	Developer             string     `yaml:"developer"`
	UnattendedInstall     bool       `yaml:"unattended_install"`
	UnattendedUninstall   bool       `yaml:"unattended_uninstall"`
	Installer             *Installer `yaml:"installer"`
	Uninstaller           *Installer `yaml:"uninstaller,omitempty"`
	SupportedArch         []string   `yaml:"supported_architectures"`
	ProductCode           string     `yaml:"product_code,omitempty"`
	UpgradeCode           string     `yaml:"upgrade_code,omitempty"`
	PreinstallScript      string     `yaml:"preinstall_script,omitempty"`
	PostinstallScript     string     `yaml:"postinstall_script,omitempty"`
	PreuninstallScript    string     `yaml:"preuninstall_script,omitempty"`
	PostuninstallScript   string     `yaml:"postuninstall_script,omitempty"`
	InstallCheckScript    string     `yaml:"installcheck_script,omitempty"`
	UninstallCheckScript  string     `yaml:"uninstallcheck_script,omitempty"`
}

// Config struct holds the configuration settings for the tool,
// such as repo path, cloud provider, and default settings.
type Config struct {
	RepoPath       string `yaml:"repo_path"`
	CloudProvider  string `yaml:"cloud_provider"`
	CloudBucket    string `yaml:"cloud_bucket"`
	DefaultCatalog string `yaml:"default_catalog"`
	DefaultArch    string `yaml:"default_arch"`
}

// Default configuration values for the tool
var defaultConfig = Config{
	RepoPath:       "./repo",
	CloudBucket:    "",
	DefaultCatalog: "testing",
	DefaultArch:    "x86_64",
}

// checkTools verifies that the required tools are installed
// depending on the operating system (Windows or macOS).
func checkTools() error {
	switch runtime.GOOS {
	case "windows":
		_, err := exec.LookPath("msiexec")
		if err != nil {
		logging.LogError(err, "Processing Error")
			return fmt.Errorf("msiexec is missing. It is needed to extract MSI metadata on Windows.")
		}
	case "darwin":
		_, err := exec.LookPath("msiextract")
		if err != nil {
		logging.LogError(err, "Processing Error")
			return fmt.Errorf("msiextract is missing. You can install it using Homebrew.")
		}
	default:
		return fmt.Errorf("Only supported on Windows and macOS.")
	}
	return nil
}

// findMatchingItem checks if there is an existing package with the same 
// name and version in the provided list of PkgsInfo.
func findMatchingItem(pkgsInfos []PkgsInfo, name string, version string) *PkgsInfo {
	for _, item := range pkgsInfos {
		if item.Name == name && item.Version == version {
			return &item
		}
	}
	return nil
}

// scanRepo scans the repository directory for existing YAML pkgsinfo files
// and loads them into a slice of PkgsInfo.
func scanRepo(repoPath string) ([]PkgsInfo, error) {
	var pkgsInfos []PkgsInfo

	err := filepath.Walk(repoPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
		logging.LogError(err, "Processing Error")
			return err
		}
		if filepath.Ext(path) == ".yaml" {
			fileContent, err := os.ReadFile(path)
			if err != nil {
		logging.LogError(err, "Processing Error")
				return err
			}
			var pkgsInfo PkgsInfo
			if err := yaml.Unmarshal(fileContent, &pkgsInfo); err != nil {
				return err
			}
			pkgsInfos = append(pkgsInfos, pkgsInfo)
		}
		return nil
	})

	return pkgsInfos, err
}

// getConfigPath determines the appropriate configuration file path
// based on the operating system.
func getConfigPath() string {
	if runtime.GOOS == "darwin" {
		return filepath.Join(os.Getenv("HOME"), "Library/Preferences/com.github.gorilla.import.yaml")
	} else if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("APPDATA"), "Gorilla", "import.yaml")
	}
	return "config.yaml" // Default path for other OSes
}

// loadConfig reads the configuration from a specified YAML file.
func loadConfig(configPath string) (Config, error) {
	var config Config
	file, err := os.Open(configPath)
	if err != nil {
		logging.LogError(err, "Processing Error")
		return config, err
	}
	defer file.Close()

	yamlDecoder := yaml.NewDecoder(file)
	if err := yamlDecoder.Decode(&config); err != nil {
		return config, err
	}

	return config, nil
}

// saveConfig writes the current configuration to a YAML file.
func saveConfig(configPath string, config Config) error {
	file, err := os.Create(configPath)
	if err != nil {
		logging.LogError(err, "Processing Error")
		return err
	}
	defer file.Close()

	encoder := yaml.NewEncoder(file)
	if err := encoder.Encode(config); err != nil {
		return err
	}

	return nil
}

// configureGorillaImport sets up the configuration for gorillaimport
// interactively, validating inputs and saving the configuration to a file.
func configureGorillaImport() Config {
    config := defaultConfig
    fmt.Println("Configuring gorillaimport...")

    // Sanity check for repo path
    for {
        fmt.Printf("Repo URL (must be an absolute path, e.g., ~/DevOps/Gorilla/deployment): ")
        fmt.Scanln(&config.RepoPath)

        // Check if the path starts with "/"
        if filepath.IsAbs(config.RepoPath) {
            break
        }
        fmt.Println("Invalid repo path. Please ensure it's an absolute path starting with '/'.")
    }

    // Validate the cloud provider
    for {
        fmt.Printf("Cloud Provider (aws/azure or leave blank for none): ")
        fmt.Scanln(&config.CloudProvider)

        config.CloudProvider = strings.ToLower(config.CloudProvider) // Normalize case
        if config.CloudProvider == "" || config.CloudProvider == "aws" || config.CloudProvider == "azure" {
            break
        }
        fmt.Println("Invalid cloud provider. Please enter 'aws', 'azure', or leave blank for none.")
    }

    // Validate the cloud bucket if cloud provider is set
    if config.CloudProvider != "" {
        for {
            fmt.Printf("Cloud Bucket (e.g., your-bucket-name/path/to/repo, no s3:// or https://): ")
            fmt.Scanln(&config.CloudBucket)

            // Check if the cloud bucket doesn't start with a protocol
            if !strings.HasPrefix(config.CloudBucket, "s3://") && !strings.HasPrefix(config.CloudBucket, "https://") {
                break
            }
            fmt.Println("Invalid cloud bucket. Please remove any 's3://' or 'https://' prefix and enter only the bucket path.")
        }
    }

    // Default catalog and architecture prompts
    fmt.Printf("Default catalog (default: %s): ", config.DefaultCatalog)
    fmt.Scanln(&config.DefaultCatalog)
    if config.DefaultCatalog == "" {
        config.DefaultCatalog = defaultConfig.DefaultCatalog
    }

    fmt.Printf("Default architecture (default: %s): ", config.DefaultArch)
    fmt.Scanln(&config.DefaultArch)
    if config.DefaultArch == "" {
        config.DefaultArch = defaultConfig.DefaultArch
    }

    // Save the configuration
    err := saveConfig(getConfigPath(), config)
    if err != nil {
		logging.LogError(err, "Processing Error")
        fmt.Printf("Error saving config: %s\n", err)
    }

    return config
}

// extractNuGetMetadata extracts metadata (ID, version, authors, description) from a .nupkg file.
func extractNuGetMetadata(nupkgPath string) (string, string, string, string, error) {
    tempDir, err := os.MkdirTemp("", "nuget-extract-")
    if err != nil {
        return "", "", "", "", fmt.Errorf("failed to create temp directory: %v", err)
    }
    defer os.RemoveAll(tempDir)

    cmd := exec.Command("nuget", "install", nupkgPath, "-OutputDirectory", tempDir, "-NoCache")
    if err := cmd.Run(); err != nil {
        return "", "", "", "", fmt.Errorf("failed to extract .nupkg: %v", err)
    }

    nuspecFiles, err := filepath.Glob(filepath.Join(tempDir, "*", "*.nuspec"))
    if err != nil || len(nuspecFiles) == 0 {
        return "", "", "", "", fmt.Errorf(".nuspec file not found")
    }

    content, err := os.ReadFile(nuspecFiles[0])
    if err != nil {
        return "", "", "", "", fmt.Errorf("failed to read .nuspec: %v", err)
    }

    var metadata Metadata
    if err := xml.Unmarshal(content, &metadata); err != nil {
        return "", "", "", "", fmt.Errorf("failed to parse .nuspec: %v", err)
    }

    return metadata.ID, metadata.Version, metadata.Authors, metadata.Description, nil
}

// extractMSIMetadata extracts metadata from an MSI file based on the platform
// (Windows or macOS) using msiexec or msidump utilities.
func extractMSIMetadata(msiFilePath string) (string, string, string, string, string, error) {
    var productName, developer, version, productCode, upgradeCode string
    tempDir, err := os.MkdirTemp("", "msi-extract-")
    if err != nil {
		logging.LogError(err, "Processing Error")
        return "", "", "", "", "", fmt.Errorf("failed to create temporary directory: %v", err)
    }
    defer os.RemoveAll(tempDir)

    switch runtime.GOOS {
    case "windows":
        // Run msiexec with the correct working directory
        msiexecCmd := exec.Command("msiexec", "/a", msiFilePath, "/qn", "TARGETDIR="+tempDir)
        msiexecCmd.Dir = tempDir // Set the working directory
        err = msiexecCmd.Run()
        if err != nil {
		logging.LogError(err, "Processing Error")
            return "", "", "", "", "", fmt.Errorf("failed to extract MSI on Windows: %v", err)
        }

    case "darwin":
        // On macOS, use msidump to extract MSI metadata
        msidumpCmd := exec.Command("msidump", msiFilePath, "-d", tempDir)
        msidumpCmd.Dir = tempDir // Set the working directory
        err = msidumpCmd.Run()
        if err != nil {
		logging.LogError(err, "Processing Error")
            return "", "", "", "", "", fmt.Errorf("failed to extract MSI on macOS: %v", err)
        }

    default:
        return "", "", "", "", "", fmt.Errorf("unsupported platform")
    }

    // Validate and parse extracted MSI data for product name, developer, and version
    summaryInfoFile := filepath.Join(tempDir, "_SummaryInformation.idt")
    if _, err := os.Stat(summaryInfoFile); os.IsNotExist(err) {
        return "", "", "", "", "", fmt.Errorf("failed to read _SummaryInformation.idt: file does not exist in %s", tempDir)
    }

    // Parse the extracted files for product metadata
    summaryData, err := os.ReadFile(summaryInfoFile)
    if err != nil {
		logging.LogError(err, "Processing Error")
        return "", "", "", "", "", fmt.Errorf("failed to read _SummaryInformation.idt: %v", err)
    }
    lines := strings.Split(string(summaryData), "\n")
    for _, line := range lines {
        cols := strings.Split(line, "\t")
        if len(cols) < 2 {
            continue
        }
        switch cols[0] {
        case "3":
            productName = cols[1]
        case "4":
            developer = cols[1]
        case "6":
            version = strings.Fields(cols[1])[0]
        }
    }

    // Parse Property.idt for product and upgrade codes
    propertyFile := filepath.Join(tempDir, "Property.idt")
    propertyData, err := os.ReadFile(propertyFile)
    if err != nil {
		logging.LogError(err, "Processing Error")
        return "", "", "", "", "", fmt.Errorf("failed to read Property.idt: %v", err)
    }
    lines = strings.Split(string(propertyData), "\n")
    for _, line := range lines {
        cols := strings.Split(line, "\t")
        if len(cols) < 2 {
            continue
        }
        switch cols[0] {
        case "ProductCode":
            productCode = cols[1]
        case "UpgradeCode":
            upgradeCode = cols[1]
        }
    }

    return productName, developer, version, productCode, upgradeCode, nil
}

// calculateSHA256 generates a SHA-256 hash for a given file path.
func calculateSHA256(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		logging.LogError(err, "Processing Error")
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

// copyFile performs file copying from source to destination, 
// creating necessary directories if they don't exist.
func copyFile(src, dst string) (int64, error) {
	destDir := filepath.Dir(dst)
	if _, err := os.Stat(destDir); os.IsNotExist(err) {
		if err := os.MkdirAll(destDir, 0755); err != nil {
			return 0, fmt.Errorf("failed to create directory '%s': %v", destDir, err)
		}
	}

	sourceFile, err := os.Open(src)
	if err != nil {
		logging.LogError(err, "Processing Error")
		return 0, err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		logging.LogError(err, "Processing Error")
		return 0, err
	}
	defer destFile.Close()

	nBytes, err := io.Copy(destFile, sourceFile)
	if err != nil {
		logging.LogError(err, "Processing Error")
		return 0, err
	}

	err = destFile.Sync()
	return nBytes, err
}

// cleanScriptInput ensures the script content is cleaned of any leading or 
// trailing whitespace characters before processing.
func cleanScriptInput(script string) string {
    cleanedScript := strings.Trim(script, " ")
    return cleanedScript
}

// indentScriptForYaml formats a script for proper YAML block scalar representation,
// including indentation and handling of empty lines.
func indentScriptForYaml(script string) string {
    lines := strings.Split(script, "\n")
    var indentedLines []string

    for _, line := range lines {
        trimmedLine := strings.TrimSpace(line)
        if trimmedLine != "" {
            indentedLines = append(indentedLines, "    " + trimmedLine)
        } else {
            // Append empty lines without indentation
            indentedLines = append(indentedLines, "") 
        }
    }

    return strings.Join(indentedLines, "\n")
}

// encodeWithSelectiveBlockScalars encodes the PkgsInfo struct to a YAML format
// while handling block scalars selectively for script fields.
func encodeWithSelectiveBlockScalars(pkgsInfo PkgsInfo) ([]byte, error) {
    // Define a slice of key-value pairs to represent the YAML fields in order
    type kv struct {
        key   string
        value interface{}
    }
    var orderedFields = []kv{
        {"name", pkgsInfo.Name},
        {"display_name", pkgsInfo.DisplayName},
        {"version", pkgsInfo.Version},
        {"catalogs", pkgsInfo.Catalogs},
        {"category", pkgsInfo.Category},
        {"description", pkgsInfo.Description},
        {"developer", pkgsInfo.Developer},
        {"installer", pkgsInfo.Installer},
        {"product_code", pkgsInfo.ProductCode},
        {"upgrade_code", pkgsInfo.UpgradeCode},
        {"supported_architectures", pkgsInfo.SupportedArch},
        {"unattended_install", pkgsInfo.UnattendedInstall},
        {"unattended_uninstall", pkgsInfo.UnattendedUninstall},
        {"preinstall_script", pkgsInfo.PreinstallScript},
        {"postinstall_script", pkgsInfo.PostinstallScript},
        {"preuninstall_script", pkgsInfo.PreuninstallScript},
        {"postuninstall_script", pkgsInfo.PostuninstallScript},
        {"installcheck_script", pkgsInfo.InstallCheckScript},
        {"uninstallcheck_script", pkgsInfo.UninstallCheckScript},
    }

    // Create a new YAML node with the ordered fields
    var rootNode yaml.Node
    rootNode.Kind = yaml.MappingNode
    for _, field := range orderedFields {
        keyNode := &yaml.Node{
            Kind:  yaml.ScalarNode,
            Tag:   "!!str",
            Value: field.key,
        }
        valueNode := &yaml.Node{}

        // Encode the value using handleScriptField for correct formatting if it's a script field
        if isScriptField(field.key) {
            if err := handleScriptField(valueNode, field.value); err != nil {
                return nil, err
            }
        } else {
            // Handle empty string values explicitly to avoid generating `""`
            if strValue, ok := field.value.(string); ok && strValue == "" {
                valueNode.Kind = yaml.ScalarNode
                valueNode.Tag = "!!null" // This will make the field appear as empty in the output
                valueNode.Value = ""
            } else {
                if err := valueNode.Encode(field.value); err != nil {
                    return nil, err
                }
            }
        }

        rootNode.Content = append(rootNode.Content, keyNode, valueNode)
    }

    // Encode the YAML node to bytes
    var buf bytes.Buffer
    encoder := yaml.NewEncoder(&buf)
    encoder.SetIndent(2)
    if err := encoder.Encode(&rootNode); err != nil {
        return nil, err
    }
    return buf.Bytes(), nil
}

// handleScriptField encodes the script field value to a YAML node 
// with appropriate formatting, using block scalars for multiline scripts.
func handleScriptField(node *yaml.Node, value interface{}) error {
    switch v := value.(type) {
    case string:
        cleanedScript := strings.TrimSpace(v)
        if cleanedScript == "" {
            // If the script content is empty, use null to indicate an empty field
            node.Kind = yaml.ScalarNode
            node.Tag = "!!null"
            node.Value = ""
        } else {
            node.Kind = yaml.ScalarNode
            node.Style = yaml.LiteralStyle // Use LiteralStyle for multiline scripts
            // Ensure the script content starts with a newline for proper block scalar formatting
            if !strings.HasPrefix(cleanedScript, "\n") {
                cleanedScript = "\n" + cleanedScript
            }
            node.Value = cleanedScript
        }
    case []string:
        // If the script is a list of strings, encode each line as a sequence
        node.Kind = yaml.SequenceNode
        for _, item := range v {
            itemNode := &yaml.Node{
                Kind:  yaml.ScalarNode,
                Tag:   "!!str",
                Value: item,
            }
            node.Content = append(node.Content, itemNode)
        }
    default:
        // Use default encoding for other types
        return node.Encode(value)
    }
    return nil
}

// addField adds a key-value pair to the given YAML node,
// handling special cases for different data types.
func addField(node *yaml.Node, key string, value interface{}) {
    keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: key}
    valueNode := &yaml.Node{Kind: yaml.ScalarNode}

    // Handle the installation struct separately
    if inst, ok := value.(*Installer); ok && inst != nil {
        valueNode.Kind = yaml.MappingNode
        addField(valueNode, "location", inst.Location)
        addField(valueNode, "hash", inst.Hash)
        addField(valueNode, "type", inst.Type)
    } else if valStr, ok := value.(string); ok && valStr != "" {
        // Set value if it's a non-empty string
        valueNode.Value = valStr
    } else if _, ok := value.(string); ok {
        // Treat empty string as null
        valueNode.Tag = "!!null"
    } else if valBool, ok := value.(bool); ok {
        // Handle boolean values
        valueNode.Value = fmt.Sprintf("%v", valBool)
        valueNode.Tag = "!!bool"
    } else if list, ok := value.([]string); ok {
        // Handle list of strings
        valueNode.Kind = yaml.SequenceNode
        for _, item := range list {
            itemNode := &yaml.Node{Kind: yaml.ScalarNode, Value: item}
            valueNode.Content = append(valueNode.Content, itemNode)
        }
    }

    node.Content = append(node.Content, keyNode, valueNode)
}

// addScriptField adds script fields to the YAML node, using block scalar formatting
// for multiline scripts to ensure proper encoding.
func addScriptField(node *yaml.Node, key string, value string) {
    keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: key}
    valueNode := &yaml.Node{Kind: yaml.ScalarNode}

    if value != "" {
        valueNode.Kind = yaml.ScalarNode
        valueNode.Style = yaml.LiteralStyle // Use block scalar style (|)
        valueNode.Value = value
    } else {
        valueNode.Kind = yaml.ScalarNode
        valueNode.Tag = "!!null"
    }

    node.Content = append(node.Content, keyNode, valueNode)
}

// getEmptyIfEmptyString returns an empty string if the input is empty, 
// otherwise returns the input as is. This helps prevent null values in output.
func getEmptyIfEmptyString(s string) interface{} {
    if s == "" {
        return "" // Can return nil to omit the field entirely
    }
    return s
}

// isScriptField checks if the provided field name corresponds to a script field,
// used for determining whether block scalar formatting is needed.
func isScriptField(fieldName string) bool {
    scriptFields := []string{
        "preinstall_script", "postinstall_script",
        "preuninstall_script", "postuninstall_script",
        "installcheck_script", "uninstallcheck_script",
    }
    for _, field := range scriptFields {
        if fieldName == field {
            return true
        }
    }
    return false
}

// populateStandardFields adds the fields from the PkgsInfo struct
// into a map to be used for YAML encoding, including optional fields like uninstaller.
func populateStandardFields(m map[string]interface{}, info PkgsInfo) {
    m["name"] = info.Name
    m["display_name"] = info.DisplayName
    m["version"] = info.Version
    m["description"] = info.Description
    m["catalogs"] = info.Catalogs
    m["category"] = info.Category
    m["developer"] = info.Developer
    m["unattended_install"] = info.UnattendedInstall
    m["unattended_uninstall"] = info.UnattendedUninstall
    m["installer"] = info.Installer
    m["supported_architectures"] = info.SupportedArch
    m["product_code"] = info.ProductCode
    m["upgrade_code"] = info.UpgradeCode
	m["preinstall_script"] = info.PreinstallScript
	m["postinstall_script"] = info.PostinstallScript
	m["preuninstall_script"] = info.PreuninstallScript 
	m["postuninstall_script"] = info.PostuninstallScript
	m["installcheck_script"] = info.InstallCheckScript
	m["uninstallcheck_script"] = info.UninstallCheckScript
    if info.Uninstaller != nil {
        m["uninstaller"] = info.Uninstaller
    }
}

// createPkgsInfo generates the pkgsinfo YAML file based on the provided metadata,
// ensuring the correct directory structure is in place and the data is properly encoded.
func createPkgsInfo(
	filePath string,
	outputDir string,
	name string,
	version string,
	catalogs []string,
	category string,
	developer string,
	supportedArch []string,
	repoPath string,
	installerSubPath string,
	productCode string,
	upgradeCode string,
	fileHash string,
	unattendedInstall bool,
	unattendedUninstall bool,
	preinstallScript string,
	postinstallScript string,
	preuninstallScript string,
	postuninstallScript string,
	installCheckScript string,
	uninstallCheckScript string,
	uninstaller *Installer,
) error {
	// Determine the installer location based on the provided subpath, name, and version
	installerLocation := filepath.Join("/", installerSubPath, fmt.Sprintf("%s-%s%s", name, version, filepath.Ext(filePath)))

	// Create a PkgsInfo struct containing all the package metadata
	pkgsInfo := PkgsInfo{
		Name:                 name,
		Version:              version,
		Installer: &Installer{
			Location: installerLocation,
			Hash:     fileHash,
			Type:     strings.TrimPrefix(filepath.Ext(filePath), "."),
		},
		Uninstaller:          uninstaller,
		Catalogs:             catalogs,
		Category:             category,
		Developer:            developer,
		Description:          "", // Optional field left blank
		SupportedArch:        supportedArch,
		ProductCode:          strings.Trim(productCode, "{}\r"),
		UpgradeCode:          strings.Trim(upgradeCode, "{}\r"),
		UnattendedInstall:    unattendedInstall,
		UnattendedUninstall:  unattendedUninstall,
		PreinstallScript:     preinstallScript,
		PostinstallScript:    postinstallScript,
		PreuninstallScript:   preuninstallScript,
		PostuninstallScript:  postuninstallScript,
		InstallCheckScript:   installCheckScript,
		UninstallCheckScript: uninstallCheckScript,
	}

	// Ensure the directory structure for the output file exists
	outputFilePath := filepath.Join(outputDir, installerSubPath)
	if _, err := os.Stat(outputFilePath); os.IsNotExist(err) {
		err = os.MkdirAll(outputFilePath, 0755)
		if err != nil {
		logging.LogError(err, "Processing Error")
			return fmt.Errorf("failed to create directory structure: %v", err)
		}
	}

	// Specify the output file path for the pkgsinfo YAML file
	outputFile := filepath.Join(outputFilePath, fmt.Sprintf("%s-%s.yaml", name, version))

	// Encode the PkgsInfo struct using the custom YAML encoding function
	pkgsInfoContent, err := encodeWithSelectiveBlockScalars(pkgsInfo)
	if err != nil {
		logging.LogError(err, "Processing Error")
	    return fmt.Errorf("failed to encode pkgsinfo YAML: %v", err)
	}
	
	// Write the encoded YAML content to the specified file
	if err := os.WriteFile(outputFile, pkgsInfoContent, 0644); err != nil {
	    return fmt.Errorf("failed to write pkgsinfo to file: %v", err)
	}

	return nil
}

// findMatchingItemInAllCatalog searches the All.yaml catalog for a package with the same product and upgrade codes,
// and checks if the package's hash matches the current file hash.
func findMatchingItemInAllCatalog(repoPath, productCode, upgradeCode, currentFileHash string) (*PkgsInfo, bool, error) {
    allCatalogPath := filepath.Join(repoPath, "catalogs", "All.yaml")
    fileContent, err := os.ReadFile(allCatalogPath)
    if err != nil {
		logging.LogError(err, "Processing Error")
        return nil, false, fmt.Errorf("failed to read All.yaml: %v", err)
    }

    var allPackages []PkgsInfo
    if err := yaml.Unmarshal(fileContent, &allPackages); err != nil {
        return nil, false, fmt.Errorf("failed to unmarshal All.yaml: %v", err)
    }

    // Clean the input productCode and upgradeCode for comparison
    cleanedProductCode := strings.Trim(strings.ToLower(productCode), "{}\r\n ")
    cleanedUpgradeCode := strings.Trim(strings.ToLower(upgradeCode), "{}\r\n ")

    // Iterate over all packages in the catalog to find a matching item
    for _, item := range allPackages {
        // Skip items where product or upgrade codes are empty
        if item.ProductCode == "" || item.UpgradeCode == "" {
            continue
        }

        // Clean the item product and upgrade codes for comparison
        itemProductCode := strings.Trim(strings.ToLower(item.ProductCode), "{}\r\n ")
        itemUpgradeCode := strings.Trim(strings.ToLower(item.UpgradeCode), "{}\r\n ")

        // Compare the cleaned product codes and upgrade codes
        if itemProductCode == cleanedProductCode && itemUpgradeCode == cleanedUpgradeCode {
            // Check if the hashes match
            if item.Installer != nil && item.Installer.Hash == currentFileHash {
                return &item, true, nil
            } else {
                return &item, false, nil
            }
        }
    }
    return nil, false, nil
}

// findMatchingItemInAllCatalogWithDifferentVersion checks the All.yaml catalog for an item with the same name but a different version.
func findMatchingItemInAllCatalogWithDifferentVersion(repoPath, name, version string) (*PkgsInfo, error) {
    allCatalogPath := filepath.Join(repoPath, "catalogs", "All.yaml")
    fileContent, err := os.ReadFile(allCatalogPath)
    if err != nil {
		logging.LogError(err, "Processing Error")
        return nil, fmt.Errorf("failed to read All.yaml: %v", err)
    }

    var allPackages []PkgsInfo
    if err := yaml.Unmarshal(fileContent, &allPackages); err != nil {
        return nil, fmt.Errorf("failed to unmarshal All.yaml: %v", err)
    }

    // Normalize input name and version for comparison
    cleanName := strings.TrimSpace(strings.ToLower(name))
    cleanVersion := strings.TrimSpace(strings.ToLower(version))

    // Iterate over all packages in the catalog
    for _, item := range allPackages {
        // Skip items with empty name or version
        if item.Name == "" || item.Version == "" {
            continue
        }

        // Normalize item name and version
        itemName := strings.TrimSpace(strings.ToLower(item.Name))
        itemVersion := strings.TrimSpace(strings.ToLower(item.Version))

        // Check if the item name matches but the version is different
        if itemName == cleanName && itemVersion != cleanVersion {
            return &item, nil
        }
    }

    return nil, nil
}

// processScript reads and processes a script file, converting line endings and wrapping if needed.
func processScript(scriptPath, wrapperType string) (string, error) {
    if scriptPath == "" {
        return "", nil
    }

    content, err := os.ReadFile(scriptPath)
    if err != nil {
        return "", fmt.Errorf("error reading script file: %v", err)
    }

    scriptContent := strings.ReplaceAll(string(content), "\r\n", "\n")

    // Wrap the script if it's a batch or PowerShell script.
    switch wrapperType {
    case "bat", "ps1":
        return generateWrapperScript(scriptContent, wrapperType), nil
    default:
        return scriptContent, nil
    }
}

// processUninstaller copies the uninstaller to the destination path and generates metadata for it.
func processUninstaller(uninstallerPath, pkgsFolderPath, installerSubPath string) (*Installer, error) {
    if uninstallerPath == "" {
        return nil, nil
    }

    if _, err := os.Stat(uninstallerPath); os.IsNotExist(err) {
        return nil, fmt.Errorf("uninstaller '%s' does not exist", uninstallerPath)
    }

    // Calculate SHA256 hash for the uninstaller.
    uninstallerHash, err := calculateSHA256(uninstallerPath)
    if err != nil {
        return nil, fmt.Errorf("error calculating uninstaller hash: %v", err)
    }

    uninstallerFilename := filepath.Base(uninstallerPath)
    uninstallerDest := filepath.Join(pkgsFolderPath, uninstallerFilename)

    // Copy the uninstaller to the repository.
    _, err = copyFile(uninstallerPath, uninstallerDest)
    if err != nil {
        return nil, fmt.Errorf("failed to copy uninstaller: %v", err)
    }

    // Return uninstaller metadata.
    return &Installer{
        Location:  filepath.Join("/", installerSubPath, uninstallerFilename),
        Hash:      uninstallerHash,
        Type:      strings.TrimPrefix(filepath.Ext(uninstallerPath), "."),
    }, nil
}

// extractInstallerMetadata determines the type of installer and extracts appropriate metadata.
func extractInstallerMetadata(packagePath string) (string, string, string, error) {
    ext := strings.ToLower(filepath.Ext(packagePath))

    switch ext {
    case ".msi":
        return extractMSIMetadata(packagePath)
    case ".nupkg":
        return extractNuGetMetadata(packagePath)
    default:
        return "", "", "", fmt.Errorf("unsupported installer type: %s", ext)
    }
}

// generatePkgsInfo creates the .pkgsinfo YAML file based on the provided metadata.
func generatePkgsInfo(config Config, installerSubPath string, info PkgsInfo) error {
    outputDir := filepath.Join(config.RepoPath, "pkgsinfo", installerSubPath)
    if err := os.MkdirAll(outputDir, 0755); err != nil {
        return fmt.Errorf("failed to create output directory: %v", err)
    }

    outputFile := filepath.Join(outputDir, fmt.Sprintf("%s-%s.yaml", info.Name, info.Version))
    pkgsInfoContent, err := encodeWithSelectiveBlockScalars(info)
    if err != nil {
        return fmt.Errorf("failed to encode pkgsinfo: %v", err)
    }

    return os.WriteFile(outputFile, pkgsInfoContent, 0644)
}

// gorillaImport processes an installer, extracts metadata, and generates a pkgsinfo file.
func gorillaImport(
    packagePath string,
    config Config,
    installScriptPath string,
    preuninstallScriptPath string,
    postuninstallScriptPath string,
    postinstallScriptPath string,
    uninstallerPath string,
    installCheckScriptPath string,
    uninstallCheckScriptPath string,
) (bool, error) {
    if _, err := os.Stat(packagePath); os.IsNotExist(err) {
        return false, fmt.Errorf("package '%s' does not exist", packagePath)
    }

    // Extract metadata from the installer.
    name, version, developer, description, err := extractInstallerMetadata(packagePath)
    if err != nil {
        return false, fmt.Errorf("metadata extraction failed: %v", err)
    }

    // Pre-process scripts.
    preinstallScript, _ := processScript(installScriptPath, filepath.Ext(installScriptPath))
    postinstallScript, _ := processScript(postinstallScriptPath, filepath.Ext(postinstallScriptPath))
    preuninstallScript, _ := processScript(preuninstallScriptPath, filepath.Ext(preuninstallScriptPath))
    postuninstallScript, _ := processScript(postuninstallScriptPath, filepath.Ext(postuninstallScriptPath))
    installCheckScript, _ := processScript(installCheckScriptPath, "")
    uninstallCheckScript, _ := processScript(uninstallCheckScriptPath, "")

    // Process uninstaller metadata.
    pkgsFolderPath := filepath.Join(config.RepoPath, "pkgs", "apps")
    uninstaller, err := processUninstaller(uninstallerPath, pkgsFolderPath, "apps")
    if err != nil {
        return false, fmt.Errorf("uninstaller processing failed: %v", err)
    }

    // Create the PkgsInfo struct.
    pkgsInfo := PkgsInfo{
        Name:                name,
        Version:             version,
        Developer:           developer,
        Description:         description,
        Installer: &Installer{
            Location: filepath.Join("/apps", filepath.Base(packagePath)),
            Hash:     calculateSHA256(packagePath),
            Type:     strings.TrimPrefix(filepath.Ext(packagePath), "."),
        },
        PreinstallScript:     preinstallScript,
        PostinstallScript:    postinstallScript,
        PreuninstallScript:   preuninstallScript,
        PostuninstallScript:  postuninstallScript,
        InstallCheckScript:   installCheckScript,
        UninstallCheckScript: uninstallCheckScript,
        Uninstaller:          uninstaller,
    }

    // Generate the pkgsinfo YAML file.
    if err := generatePkgsInfo(config, "apps", pkgsInfo); err != nil {
        return false, fmt.Errorf("failed to generate pkgsinfo: %v", err)
    }

    fmt.Printf("Pkgsinfo created at: /apps/%s-%s.yaml\n", name, version)
    return true, nil
}

// generateWrapperScript generates a wrapper script to execute a given script content.
// For batch (.bat) scripts, it creates a temporary file and executes it using cmd.exe.
// For PowerShell (.ps1) scripts, it returns the original script content without wrapping.
func generateWrapperScript(batchContent, scriptType string) string {
    if scriptType == "bat" {
        // Format a batch script wrapper that writes the content to a temporary file, runs it, and deletes the file.
        return fmt.Sprintf(`
$batchScriptContent = @'
%s
'@

$batchFile = "$env:TEMP\\temp_script.bat"
Set-Content -Path $batchFile -Value $batchScriptContent -Encoding ASCII
& cmd.exe /c $batchFile
Remove-Item $batchFile
        `, strings.TrimLeft(batchContent, " ")) // Trim leading spaces from batchContent to avoid formatting issues.
    } else if scriptType == "ps1" {
        // Return the content as is for PowerShell scripts, no wrapping needed.
        return batchContent
    } else {
        // Return an empty string for unsupported script types.
        return ""
    }
}

// Custom prompt template configuration to suppress the default "?" icon in survey prompts.
var customPromptTemplate = survey.IconSet{
    Question: survey.Icon{
        Text: "", // No icon for question prompts.
    },
}

// promptSurvey prompts the user with a given message and default value using the survey library.
// It allows the user to modify the value, and the cleaned input is assigned back to the original variable.
func promptSurvey(value *string, prompt string, defaultValue string) {
    // Clean the default value before showing the prompt.
    cleanDefault := cleanTextForPrompt(defaultValue)

    // Use the survey library to ask for input, applying custom icons to suppress default symbols.
    survey.AskOne(&survey.Input{
        Message: prompt,
        Default: cleanDefault,
    }, value, survey.WithIcons(func(icons *survey.IconSet) {
        *icons = customPromptTemplate
    }))
}

// getInputWithDefault asks the user for input with a default value shown in square brackets.
// If the user doesn't provide input, the default value is returned.
func getInputWithDefault(prompt, defaultValue string) string {
    // Clean the default value for display.
    cleanDefault := cleanTextForPrompt(defaultValue)

    // Print the prompt with the default value if it's not empty.
    if cleanDefault != "" {
        fmt.Printf("%s [%s]: ", prompt, cleanDefault)
    } else {
        fmt.Printf("%s: ", prompt)
    }

    // Read user input from the command line.
    var input string
    fmt.Scanln(&input)

    // If the input is empty, use the default value; otherwise, return the provided input.
    if input == "" {
        return cleanDefault
    }
    return input
}

// cleanTextForPrompt trims any whitespace from the input string to ensure it's suitable for use in prompts.
func cleanTextForPrompt(input string) string {
    return strings.TrimSpace(input)
}

// confirmAction prompts the user with a yes/no question and returns true if the response is affirmative (y/yes).
func confirmAction(prompt string) bool {
    fmt.Printf("%s (y/n): ", prompt)
    var response string
    _, err := fmt.Scanln(&response)
    if err != nil {
		logging.LogError(err, "Processing Error")
        fmt.Println("Error reading input, assuming 'no'")
        return false
    }

    // Normalize the response to lowercase and check for affirmative answers.
    response = strings.ToLower(strings.TrimSpace(response))
    return response == "y" || response == "yes"
}

// uploadToCloud manages the uploading of files to a cloud storage provider (AWS S3 or Azure Blob Storage).
// It uses the appropriate tool (AWS CLI or AzCopy) to perform the upload based on the configured cloud provider.
func uploadToCloud(config Config) error {
    // If no cloud provider is configured, skip the upload process.
    if config.CloudProvider == "none" {
        return nil
    }

    // Construct the path to the local pkgs directory based on the RepoPath.
    localPkgsPath := filepath.Join(config.RepoPath, "pkgs")

    // Perform the appropriate upload process based on the cloud provider (AWS or Azure).
    if config.CloudProvider == "aws" {
        // Verify that the AWS CLI is installed.
        awsPath := "/usr/local/bin/aws"
        if _, err := os.Stat(awsPath); os.IsNotExist(err) {
            fmt.Println("AWS CLI not found at /usr/local/bin/aws. Please install AWS CLI.")
            return err
        }

        // Ensure AWS credentials are properly set up by checking the caller identity.
        awsCheckCmd := exec.Command(awsPath, "sts", "get-caller-identity")
        if err := awsCheckCmd.Run(); err != nil {
            fmt.Println("AWS CLI not properly configured or logged in. Please run `aws configure`.")
            return err
        }

        // Use the AWS CLI to sync the local pkgs directory to the specified S3 bucket.
        fmt.Println("Starting upload for pkgs to AWS S3")
        cmd := exec.Command(awsPath, "s3", "sync",
            localPkgsPath,
            fmt.Sprintf("s3://%s/pkgs/", config.CloudBucket),
            "--exclude", "*.git/*", "--exclude", "**/.DS_Store")

        // Redirect the command's output and error streams to the console.
        cmd.Stdout = os.Stdout
        cmd.Stderr = os.Stderr

        // Run the command and check for errors.
        if err := cmd.Run(); err != nil {
            return fmt.Errorf("error syncing pkgs directory to S3: %v", err)
        }
        fmt.Println("Upload to S3 completed successfully")

    } else if config.CloudProvider == "azure" {
        // Verify that AzCopy is installed.
        azcopyPath := "/opt/homebrew/bin/azcopy"
        if _, err := os.Stat(azcopyPath); os.IsNotExist(err) {
            fmt.Println("AzCopy not found at /opt/homebrew/bin/azcopy. Please install AzCopy.")
            return err
        }

        // Ensure the user is logged in to Azure.
        azureCheckCmd := exec.Command("/opt/homebrew/bin/az", "account", "show")
        if err := azureCheckCmd.Run(); err != nil {
            fmt.Println("AzCopy not properly configured or logged in. Please run `az login`.")
            return err
        }

        // Use AzCopy to sync the local pkgs directory to the specified Azure Blob Storage.
        fmt.Println("Starting upload for pkgs to Azure Blob Storage")
        cmd := exec.Command(azcopyPath, "sync",
            localPkgsPath,
            fmt.Sprintf("https://%s/pkgs/", config.CloudBucket),
            "--exclude-path", "*/.git/*;*/.DS_Store", "--recursive", "--put-md5")

        // Redirect the command's output and error streams to the console.
        cmd.Stdout = os.Stdout
        cmd.Stderr = os.Stderr

        // Run the command and check for errors.
        if err := cmd.Run(); err != nil {
            return fmt.Errorf("error syncing pkgs directory to Azure Blob Storage: %v", err)
        }
        fmt.Println("Upload to Azure completed successfully")
    }

    return nil
}

// rebuildCatalogs is a stub function that currently does nothing but prints a message.
// This function is a placeholder for catalog rebuilding logic that can be implemented later.
func rebuildCatalogs() {
    fmt.Println("Rebuild catalogs not implemented yet.")
}

// main is the entry point of the program, handling configuration and running gorillaImport.
func main() {
	logging.InitLogger()
	defer logging.CloseLogger()
    // Define command-line flags for various options.
    configFlag := flag.Bool("config", false, "Run interactive configuration setup.")
    archFlag := flag.String("arch", "", "Specify the architecture (e.g., x86_64, arm64)")
    installerFlag := flag.String("installer", "", "Path to the installer .exe or .msi file.")
    uninstallerFlag := flag.String("uninstaller", "", "Path to the uninstaller .exe or .msi file.")
    installScriptFlag := flag.String("installscript", "", "Path to the install script (.bat or .ps1).")
    preuninstallScriptFlag := flag.String("preuninstallscript", "", "Path to the preuninstall script (.bat or .ps1).")
    postuninstallScriptFlag := flag.String("postuninstallscript", "", "Path to the postuninstall script (.bat or .ps1).")
    postinstallScriptFlag := flag.String("postinstallscript", "", "Path to the post-install script (.ps1).")
    installCheckScriptFlag := flag.String("installcheckscript", "", "Path to the install check script.")
    uninstallCheckScriptFlag := flag.String("uninstallcheckscript", "", "Path to the uninstall check script.")
    flag.Parse()

    // Run configuration setup if the config flag is set.
    if *configFlag {
        configureGorillaImport()
        return
    }

    // Verify that necessary tools are installed before proceeding.
    if err := checkTools(); err != nil {
        fmt.Printf("Error: %s\n", err)
        os.Exit(1)
    }

    // Initialize the configuration using defaults and attempt to load any saved configuration.
    configData := defaultConfig
    configPath := getConfigPath()

    // Load existing configuration from a file if available.
    if _, err := os.Stat(configPath); err == nil {
        loadedConfig, err := loadConfig(configPath)
        if err == nil {
            // Override the default configuration with values from the loaded config, if they are set.
            if loadedConfig.RepoPath != "" {
                configData.RepoPath = loadedConfig.RepoPath
            }
            if loadedConfig.DefaultCatalog != "" {
                configData.DefaultCatalog = loadedConfig.DefaultCatalog
            }
            if loadedConfig.DefaultArch != "" {
                configData.DefaultArch = loadedConfig.DefaultArch
            }
            if loadedConfig.CloudBucket != "" {
                configData.CloudBucket = loadedConfig.CloudBucket
            }
            if loadedConfig.CloudProvider != "" {
                configData.CloudProvider = loadedConfig.CloudProvider
            }
        }
    }

    // Determine the package path from command-line arguments or prompt the user.
    var packagePath string
    if *installerFlag != "" {
        packagePath = *installerFlag
    } else if flag.NArg() > 0 {
        packagePath = flag.Arg(0)
    } else {
        fmt.Printf("Enter the path to the package file to import: ")
        fmt.Scanln(&packagePath)
    }

    // Override the default architecture if the --arch flag is provided.
    if *archFlag != "" {
        configData.DefaultArch = *archFlag
    }

    // Perform the import process and check if it was successful.
    importSuccess, err := gorillaImport(
        packagePath,
        *installScriptFlag,
        *preuninstallScriptFlag,
        *postuninstallScriptFlag,
        *postinstallScriptFlag,
        *uninstallerFlag,
        *installCheckScriptFlag,
        *uninstallCheckScriptFlag,
        configData,
    )
    if err != nil {
		logging.LogError(err, "Processing Error")
        fmt.Printf("Error: %s\n", err)
        os.Exit(1)
    }

    // If the import was successful and a cloud provider is configured, upload the package.
    if importSuccess && configData.CloudProvider != "none" {
        if err := uploadToCloud(configData); err != nil {
            fmt.Printf("Error uploading to cloud: %s\n", err)
        }
    }

    // After a successful import, prompt the user to rebuild the catalogs.
    if importSuccess {
        confirm := getInputWithDefault("Would you like to run makecatalogs? [y/n]", "n")
        if strings.ToLower(confirm) == "y" {
            fmt.Println("Running makecatalogs to update catalogs...")

            // Execute the makecatalogs command.
            makeCatalogsBinary := filepath.Join(filepath.Dir(os.Args[0]), "makecatalogs")
            cmd := exec.Command(makeCatalogsBinary)
            cmd.Stdout = os.Stdout
            cmd.Stderr = os.Stderr

            // Check for errors during catalog rebuild.
            err := cmd.Run()
            if err != nil {
		logging.LogError(err, "Processing Error")
                fmt.Printf("Error running makecatalogs: %v\n", err)
                os.Exit(1)
            }

            fmt.Println("makecatalogs completed successfully.")
        } else {
            fmt.Println("Skipped running makecatalogs.")
        }
    }
}
