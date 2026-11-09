// Modified by Nanostack from go-jet (https://github.com/go-jet/jet), commit d0e152c.
// See db/UPSTREAM.md for the upstream base and the list of changes.

package template

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/nanostack-dev/nanostack-framework/db/generator/metadata"
	"github.com/nanostack-dev/nanostack-framework/db/internal/jet"
)

// ProcessSchema will process schema metadata and constructs go files using generator Template
func ProcessSchema(dirPath string, schemaMetaData metadata.Schema, generatorTemplate Template) error {
	if schemaMetaData.IsEmpty() {
		return nil
	}

	schemaTemplate := generatorTemplate.Schema(schemaMetaData)
	schemaPath := filepath.Join(dirPath, schemaTemplate.Path)

	fmt.Println("Destination directory:", schemaPath)
	fmt.Println("Cleaning up destination directory...")
	if err := os.RemoveAll(schemaPath); err != nil {
		return errors.New("failed to cleanup generated files")
	}

	return processSchema(diskSink{}, schemaPath, schemaMetaData, generatorTemplate, schemaTemplate)
}

// processSchema renders the model and SQL builder files of one schema into out.
func processSchema(out sink, schemaPath string, schemaMetaData metadata.Schema, generatorTemplate Template, schemaTemplate Schema) error {
	err := processModel(out, schemaPath, schemaMetaData, schemaTemplate, generatorTemplate.header())
	if err != nil {
		return fmt.Errorf("failed to generate model types: %w", err)
	}

	err = processSQLBuilder(out, schemaPath, generatorTemplate.Dialect, schemaMetaData, schemaTemplate, generatorTemplate.header())
	if err != nil {
		return fmt.Errorf("failed to generate sql builder types: %w", err)
	}

	return nil
}

func processModel(out sink, dirPath string, schemaMetaData metadata.Schema, schemaTemplate Schema, header string) error {
	modelTemplate := schemaTemplate.Model

	if modelTemplate.Skip {
		out.logf("Skipping the generation of model types.\n")
		return nil
	}

	modelDirPath := filepath.Join(dirPath, modelTemplate.Path)

	err := out.ensureDir(modelDirPath)
	if err != nil {
		return fmt.Errorf("destination dir path does not exist: %w", err)
	}

	err = processTableModels(out, header, "table", modelDirPath, schemaMetaData.TablesMetaData, modelTemplate)
	if err != nil {
		return fmt.Errorf("failed to generate table model types: %w", err)
	}

	err = processTableModels(out, header, "view", modelDirPath, schemaMetaData.ViewsMetaData, modelTemplate)
	if err != nil {
		return fmt.Errorf("failed to generate view model types: %w", err)
	}

	err = processEnumModels(out, header, modelDirPath, schemaMetaData.EnumsMetaData, modelTemplate)
	if err != nil {
		return fmt.Errorf("failed to process enum types: %w", err)
	}

	return nil
}

func processSQLBuilder(out sink, dirPath string, dialect jet.Dialect, schemaMetaData metadata.Schema, schemaTemplate Schema, header string) error {
	sqlBuilderTemplate := schemaTemplate.SQLBuilder

	if sqlBuilderTemplate.Skip {
		out.logf("Skipping the generation of SQL Builder types.\n")
		return nil
	}

	sqlBuilderPath := filepath.Join(dirPath, sqlBuilderTemplate.Path)

	err := processTableSQLBuilder(out, header, "table", sqlBuilderPath, dialect, schemaMetaData, schemaMetaData.TablesMetaData, sqlBuilderTemplate)
	if err != nil {
		return fmt.Errorf("failed to process table sql builder types: %w", err)
	}

	err = processTableSQLBuilder(out, header, "view", sqlBuilderPath, dialect, schemaMetaData, schemaMetaData.ViewsMetaData, sqlBuilderTemplate)
	if err != nil {
		return fmt.Errorf("failed to process view sql builder types: %w", err)
	}

	err = processEnumSQLBuilder(out, header, sqlBuilderPath, dialect, schemaMetaData.EnumsMetaData, sqlBuilderTemplate)
	if err != nil {
		return fmt.Errorf("failed to process enum types: %w", err)
	}

	return nil
}

func processEnumSQLBuilder(out sink, header, dirPath string, dialect jet.Dialect, enumsMetaData []metadata.Enum, sqlBuilder SQLBuilder) error {
	if len(enumsMetaData) == 0 {
		return nil
	}

	out.logf("Generating enum sql builder files\n")

	for _, enumMetaData := range enumsMetaData {
		enumTemplate := sqlBuilder.Enum(enumMetaData)

		if enumTemplate.Skip {
			continue
		}

		enumSQLBuilderPath := filepath.Join(dirPath, enumTemplate.Path)

		err := out.ensureDir(enumSQLBuilderPath)
		if err != nil {
			return fmt.Errorf("failed to create enum sql builder directory - %s: %w", enumSQLBuilderPath, err)
		}

		text, err := generateTemplate(
			header+enumSQLBuilderTemplate,
			enumMetaData,
			template.FuncMap{
				"package": func() string {
					return enumTemplate.PackageName()
				},
				"dialect": func() jet.Dialect {
					return dialect
				},
				"enumTemplate": func() EnumSQLBuilder {
					return enumTemplate
				},
				"enumValueName": func(enumValue string) string {
					return enumTemplate.ValueName(enumValue)
				},
				"golangComment": formatGolangComment,
			})
		if err != nil {
			return fmt.Errorf("failed to generete enum type %s: %w", enumTemplate.FileName, err)
		}

		err = out.save(enumSQLBuilderPath, enumTemplate.FileName, text)
		if err != nil {
			return fmt.Errorf("failed to format and save '%s' enum type : %w", enumTemplate.FileName, err)
		}
	}

	return nil
}

func processTableSQLBuilder(out sink, header, fileTypes, dirPath string,
	dialect jet.Dialect,
	schemaMetaData metadata.Schema,
	tablesMetaData []metadata.Table,
	sqlBuilderTemplate SQLBuilder) error {

	if len(tablesMetaData) == 0 {
		return nil
	}

	out.logf("Generating %s sql builder files\n", fileTypes)

	var generatedBuilders []TableSQLBuilder

	for _, tableMetaData := range tablesMetaData {
		var tableSQLBuilder TableSQLBuilder

		if fileTypes == "view" {
			tableSQLBuilder = sqlBuilderTemplate.View(tableMetaData)
		} else {
			tableSQLBuilder = sqlBuilderTemplate.Table(tableMetaData)
		}

		if tableSQLBuilder.Skip {
			continue
		}

		tableSQLBuilderPath := filepath.Join(dirPath, tableSQLBuilder.Path)

		err := out.ensureDir(tableSQLBuilderPath)
		if err != nil {
			return fmt.Errorf("failed to create table sql builder directory - %s: %w", tableSQLBuilderPath, err)
		}

		text, err := generateTemplate(
			header+tableSQLBuilderTemplate,
			tableMetaData,
			template.FuncMap{
				"package": func() string {
					return tableSQLBuilder.PackageName()
				},
				"dialect": func() jet.Dialect {
					return dialect
				},
				"schemaName": func() string {
					return schemaMetaData.Name
				},
				"tableTemplate": func() TableSQLBuilder {
					return tableSQLBuilder
				},
				"structImplName": func() string { // postgres only
					structName := tableSQLBuilder.TypeName
					return string(strings.ToLower(structName)[0]) + structName[1:]
				},
				"columnField": func(columnMetaData metadata.Column) TableSQLBuilderColumn {
					return tableSQLBuilder.Column(columnMetaData)
				},
				"toUpper": strings.ToUpper,
				"insertedRowAlias": func() string {
					return insertedRowAlias(dialect)
				},
				"golangComment": formatGolangComment,
				"uniqueKeys": func() (string, error) { // Nanostack: see unique_keys.go
					if fileTypes == "view" || !tableSQLBuilder.UniqueKeys {
						return "", nil
					}
					return uniqueKeysMethod(tableMetaData, tableSQLBuilder)
				},
				"columnTypes": func() (string, error) { // Nanostack: see catalog_methods.go
					if fileTypes == "view" || !tableSQLBuilder.ColumnTypes {
						return "", nil
					}
					return columnTypesMethod(tableMetaData, tableSQLBuilder, dialect)
				},
				"foreignKeys": func() (string, error) { // Nanostack: see catalog_methods.go
					if fileTypes == "view" || !tableSQLBuilder.ForeignKeys {
						return "", nil
					}
					return foreignKeysMethod(tableMetaData, tableSQLBuilder, dialect, schemaMetaData.Name)
				},
				"columnList": func(columns []metadata.Column) string {
					names := []string{}
					for _, col := range columns {
						bc := tableSQLBuilder.Column(col)
						if bc.Skip {
							continue
						}
						names = append(names, bc.Name+"Column")
					}
					return strings.Join(names, ", ")
				},
			})
		if err != nil {
			return fmt.Errorf("failed to generate table sql builder type %s: %w", tableSQLBuilder.TypeName, err)
		}

		err = out.save(tableSQLBuilderPath, tableSQLBuilder.FileName, text)
		if err != nil {
			return fmt.Errorf("failed to format and save generated sql builder type '%s': %w", tableSQLBuilder.FileName, err)
		}

		generatedBuilders = append(generatedBuilders, tableSQLBuilder)
	}

	err := generateUseSchemaFunc(out, header, dirPath, fileTypes, generatedBuilders)
	if err != nil {
		return fmt.Errorf("failed to generate UseSchema function")
	}

	return nil
}

func generateUseSchemaFunc(out sink, header, dirPath, fileTypes string, builders []TableSQLBuilder) error {
	if len(builders) == 0 {
		return nil
	}

	text, err := generateTemplate(
		header+tableSqlBuilderSetSchemaTemplate,
		builders,
		template.FuncMap{
			"package": func() string { return builders[0].PackageName() },
			"type":    func() string { return fileTypes },
		},
	)
	if err != nil {
		return fmt.Errorf("failed to generate use schema template: %w", err)
	}

	basePath := filepath.Join(dirPath, builders[0].Path)
	fileName := fileTypes + "_use_schema"

	err = out.save(basePath, fileName, text)
	if err != nil {
		return fmt.Errorf("failed to save %s file: %w", fileName, err)
	}

	return nil
}

func insertedRowAlias(dialect jet.Dialect) string {
	if dialect.Name() == "MySQL" {
		return "new"
	}

	return "excluded"
}

func processTableModels(out sink, header, fileTypes, modelDirPath string, tablesMetaData []metadata.Table, modelTemplate Model) error {
	if len(tablesMetaData) == 0 {
		return nil
	}
	out.logf("Generating %s model files...\n", fileTypes)

	for _, tableMetaData := range tablesMetaData {
		var tableTemplate TableModel

		if fileTypes == "table" {
			tableTemplate = modelTemplate.Table(tableMetaData)
		} else {
			tableTemplate = modelTemplate.View(tableMetaData)
		}

		if tableTemplate.Skip {
			continue
		}

		text, err := generateTemplate(
			header+tableModelFileTemplate,
			tableMetaData,
			template.FuncMap{
				"package": func() string {
					return modelTemplate.PackageName()
				},
				"modelImports": func() []string {
					return getTableModelImports(tableTemplate, tableMetaData)
				},
				"tableTemplate": func() TableModel {
					return tableTemplate
				},
				"structField": func(columnMetaData metadata.Column) TableModelField {
					return addColumnTag(
						tableTemplate.Field(columnMetaData),
						columnMetaData,
					)
				},
				"golangComment": formatGolangComment,
			})
		if err != nil {
			return fmt.Errorf("failed to generate model type '%s': %w", tableMetaData.Name, err)
		}

		err = out.save(modelDirPath, tableTemplate.FileName, text)
		if err != nil {
			return fmt.Errorf("failed to save '%s' model type: %w", tableTemplate.FileName, err)
		}
	}

	return nil
}

func processEnumModels(out sink, header, modelDir string, enumsMetaData []metadata.Enum, modelTemplate Model) error {
	if len(enumsMetaData) == 0 {
		return nil
	}
	out.logf("Generating enum model files...\n")

	for _, enumMetaData := range enumsMetaData {
		enumTemplate := modelTemplate.Enum(enumMetaData)

		if enumTemplate.Skip {
			continue
		}

		text, err := generateTemplate(
			header+enumModelTemplate,
			enumMetaData,
			template.FuncMap{
				"package": func() string {
					return modelTemplate.PackageName()
				},
				"enumTemplate": func() EnumModel {
					return enumTemplate
				},
				"valueName": func(value string) string {
					return enumTemplate.ValueName(value)
				},
				"golangComment": formatGolangComment,
			})

		if err != nil {
			return fmt.Errorf("failed to generate enum type '%s': %w", enumMetaData.Name, err)
		}

		err = out.save(modelDir, enumTemplate.FileName, text)
		if err != nil {
			return fmt.Errorf("failed to save '%s' enum type: %w", enumTemplate.FileName, err)
		}
	}

	return nil
}

func generateTemplate(templateText string, templateData interface{}, funcMap template.FuncMap) ([]byte, error) {
	t, err := template.New("sqlBuilderTableTemplate").Funcs(funcMap).Parse(templateText)

	if err != nil {
		return nil, fmt.Errorf("failed to parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, templateData); err != nil {
		return nil, fmt.Errorf("failed to generate template: %w", err)
	}

	return buf.Bytes(), nil
}
