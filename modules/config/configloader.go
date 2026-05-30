package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

type Loader interface {
	Init(configPath string, dotEnvPath string) error
	LoadConfig(configKey string, cfg interface{}) error
	MustLoadConfig(configKey string, cfg interface{})
	Clean()
}

type LoaderImpl struct {
	nanostackConfig map[string]interface{}
	configRegistry  map[string]interface{}
}

func NewConfigLoader() *LoaderImpl {
	return &LoaderImpl{configRegistry: make(map[string]interface{})}
}

func (c *LoaderImpl) Init(configPath string, dotEnvPath string) error {
	c.nanostackConfig = make(map[string]interface{})

	envFile := os.Getenv("ENV_FILE")
	if envFile == "" {
		envFile = filepath.Join(dotEnvPath, ".env")
	}
	if _, statErr := os.Stat(envFile); statErr == nil {
		if loadErr := godotenv.Load(envFile); loadErr != nil {
			return fmt.Errorf("error loading .env file: %w", loadErr)
		}
	}

	re := regexp.MustCompile(`\$\{([^}:\s]+)(?::([^}\s]*))?\}`)
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file %s: %w", configPath, err)
	}
	processedData, err := c.replacePlaceholders(string(data), re)
	if err != nil {
		return fmt.Errorf("error processing placeholders in %s: %w", configPath, err)
	}
	if err := yaml.Unmarshal([]byte(processedData), &c.nanostackConfig); err != nil {
		return fmt.Errorf("failed to parse YAML in %s: %w", configPath, err)
	}
	return nil
}

func (c *LoaderImpl) replacePlaceholders(data string, re *regexp.Regexp) (string, error) {
	var missingVars []string
	var errs []error
	result := re.ReplaceAllStringFunc(data, func(match string) string {
		groups := re.FindStringSubmatch(match)
		varName := groups[1]
		defaultValue := groups[2]
		value, exists := os.LookupEnv(varName)
		if !exists {
			fileVarName := varName + "_FILE"
			if filePath, fileExists := os.LookupEnv(fileVarName); fileExists {
				fileData, err := os.ReadFile(filePath)
				if err != nil {
					errs = append(errs, fmt.Errorf("failed to read secret file for %s from path %s: %w", varName, filePath, err))
					return ""
				}
				value = strings.TrimSpace(string(fileData))
			} else if defaultValue != "" {
				value = defaultValue
			} else {
				missingVars = append(missingVars, varName)
				value = ""
			}
		}
		return value
	})
	if len(errs) > 0 {
		return "", errors.Join(errs...)
	}
	if len(missingVars) > 0 {
		return "", fmt.Errorf("missing required environment variables: %v", missingVars)
	}
	return result, nil
}

func (c *LoaderImpl) LoadConfig(configKey string, cfg interface{}) error {
	if cached, exists := c.configRegistry[configKey]; exists {
		reflect.ValueOf(cfg).Elem().Set(reflect.ValueOf(cached))
		return nil
	}
	subConfig, exists := c.nanostackConfig[configKey]
	if !exists {
		return fmt.Errorf("configuration key %q not found", configKey)
	}
	subConfigYAML, err := yaml.Marshal(subConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal sub-config: %w", err)
	}
	if err := yaml.Unmarshal(subConfigYAML, cfg); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}
	if err := c.validateConfig(cfg); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}
	c.configRegistry[configKey] = reflect.ValueOf(cfg).Elem().Interface()
	return nil
}

func (c *LoaderImpl) MustLoadConfig(configKey string, cfg interface{}) {
	if err := c.LoadConfig(configKey, cfg); err != nil {
		panic(fmt.Sprintf("failed to load required config %q: %v", configKey, err))
	}
}

func (c *LoaderImpl) validateConfig(configStruct interface{}) error {
	v := reflect.ValueOf(configStruct)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return errors.New("configStruct must be a non-nil pointer")
	}
	v = v.Elem()
	t := v.Type()
	var missingFields []string
	c.checkStruct(t, v, "", &missingFields)
	if len(missingFields) > 0 {
		return fmt.Errorf("missing required configuration fields: %v", missingFields)
	}
	return nil
}

func (c *LoaderImpl) checkStruct(t reflect.Type, v reflect.Value, parent string, missingFields *[]string) {
	for i := range t.NumField() {
		field := t.Field(i)
		fieldValue := v.Field(i)
		if !fieldValue.CanInterface() {
			continue
		}
		fullName := c.getFieldFullName(field, parent)
		if field.Type.Kind() == reflect.Struct {
			c.checkStruct(field.Type, fieldValue, fullName, missingFields)
			continue
		}
		if field.Type.Kind() == reflect.Pointer {
			if fieldValue.IsNil() {
				*missingFields = append(*missingFields, fullName)
				continue
			}
			fieldValue = fieldValue.Elem()
		}
		c.checkFieldValue(field, fieldValue, fullName, missingFields)
	}
}

func (c *LoaderImpl) getFieldFullName(field reflect.StructField, parent string) string {
	yamlTag := field.Tag.Get("yaml")
	if yamlTag == "" {
		yamlTag = field.Name
	}
	fullName := yamlTag
	if parent != "" {
		fullName = parent + "." + yamlTag
	}
	return fullName
}

func (c *LoaderImpl) checkFieldValue(field reflect.StructField, fieldValue reflect.Value, fullName string, missingFields *[]string) {
	optional := field.Tag.Get("optional") == "true"
	if !optional && c.isZeroValue(fieldValue) {
		*missingFields = append(*missingFields, fullName)
	}
}

func (c *LoaderImpl) isZeroValue(fieldValue reflect.Value) bool {
	switch fieldValue.Kind() {
	case reflect.String:
		return fieldValue.String() == ""
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return fieldValue.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return fieldValue.Uint() == 0
	case reflect.Bool:
		return !fieldValue.Bool()
	case reflect.Float32, reflect.Float64:
		return fieldValue.Float() == 0
	default:
		return false
	}
}

func (c *LoaderImpl) Clean() {
	c.nanostackConfig = nil
	c.configRegistry = make(map[string]interface{})
}
