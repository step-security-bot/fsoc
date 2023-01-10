package solution

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/apex/log"
	"github.com/spf13/afero"
	"github.com/spf13/afero/zipfs"
	"github.com/spf13/cobra"

	"github.com/cisco-open/fsoc/output"
	"github.com/cisco-open/fsoc/platform/api"
)

var solutionForkCmd = &cobra.Command{
	Use:   "fork --name=<solutionName> --source-name=<sourceSolutionName> [--stage=STABLE|TEST]",
	Short: "Fork a solution in the specified folder",
	Long:  `This command will download the solution into this folder and change the name of the manifest to the specified name`,
	Run:   solutionForkCommand,
}

func GetSolutionForkCommand() *cobra.Command {
	solutionForkCmd.Flags().String("source-name", "", "name of the solution that needs to be downloaded")
	_ = solutionForkCmd.MarkFlagRequired("source-name")
	solutionForkCmd.Flags().String("name", "", "name of the solution to copy it to")
	_ = solutionForkCmd.MarkFlagRequired("name")
	solutionForkCmd.Flags().String("stage", "STABLE", "The pipeline stage[STABLE or TEST] of solution that needs to be downloaded. Default value is STABLE")
	return solutionForkCmd
}

func solutionForkCommand(cmd *cobra.Command, args []string) {
	solutionName, _ := cmd.Flags().GetString("source-name")
	forkName, _ := cmd.Flags().GetString("name")
	if solutionName == "" {
		log.Fatalf("name cannot be empty, use --source-name=<solution-name>")
	}

	var stage string
	stage, _ = cmd.Flags().GetString("stage")
	if stage != "STABLE" && stage != "TEST" {
		log.Fatalf("%s isn't a valid value for the --stage flag. Possible values are TEST or STABLE", stage)
	}

	currentDirectory, err := filepath.Abs(".")
	if err != nil {
		log.Fatalf("Error getting current directory: %v", currentDirectory)
	}

	fileSystem := afero.NewBasePathFs(afero.NewOsFs(), currentDirectory)

	if manifestExists(fileSystem) {
		log.Fatalf("There is already a manifest file in this folder")
	}

	downloadSolutionZip(solutionName, stage, forkName)
	err = extractZip(fileSystem, solutionName)
	if err != nil {
		log.Fatalf("Failed to copy files from the zip file to current directory: %v", err)
	}

	editManifest(fileSystem, forkName)

	err = fileSystem.Remove("./" + solutionName + ".zip")
	if err != nil {
		log.Fatalf("Failed to zip file in current directory: %v", err)
	}

	message := fmt.Sprintf("Successfully forked %s to current directory.\r\n", solutionName)
	output.PrintCmdStatus(message)

}

func manifestExists(fileSystem afero.Fs) bool {
	exists, err := afero.Exists(fileSystem, "manifest.json")
	if err != nil {
		log.Fatalf("Failed to read filesystem for manifest: %v", err)
	}
	return exists
}

func extractZip(fileSystem afero.Fs, solutionName string) error {
	zipFile, err := fileSystem.OpenFile("./"+solutionName+".zip", os.O_RDONLY, os.FileMode(0644))
	if err != nil {
		log.Fatalf("Error opening zip file: %v", err)
	}
	fileInfo, _ := fileSystem.Stat("./" + solutionName + ".zip")
	reader, _ := zip.NewReader(zipFile, fileInfo.Size())
	zipFileSystem := zipfs.New(reader)
	err = copyFolderToLocal(zipFileSystem, fileSystem, "./"+solutionName)
	return err
}

func editManifest(fileSystem afero.Fs, forkName string) {
	manifestFile, err := afero.ReadFile(fileSystem, "./manifest.json")
	if err != nil {
		log.Fatalf("Error opening manifest file")
	}

	var manifest Manifest
	err = json.Unmarshal(manifestFile, &manifest)
	if err != nil {
		log.Errorf("Failed to parse solution manifest: %v", err)
	}
	manifest.Name = forkName

	b, _ := json.Marshal(manifest)

	err = afero.WriteFile(fileSystem, "./manifest.json", b, 0644)
	if err != nil {
		log.Errorf("Failed to to write to solution manifest: %v", err)
	}
}

func downloadSolutionZip(solutionName string, stage string, forkName string) {
	var solutionNameWithZipExtension = getSolutionNameWithZip(solutionName)
	var message string

	headers := map[string]string{
		"stage":            stage,
		"solutionFileName": solutionNameWithZipExtension,
	}
	httpOptions := api.Options{Headers: headers}
	bufRes := make([]byte, 0)
	if err := api.HTTPGet(getSolutionDownloadUrl(solutionName), &bufRes, &httpOptions); err != nil {
		log.Fatalf("Solution download command failed: %v", err.Error())
	}

	message = fmt.Sprintf("Solution bundle %s was successfully downloaded in the this directory.\r\n", solutionName)
	output.PrintCmdStatus(message)

	message = fmt.Sprintf("Changing solution name in manifest to %s.\r\n", forkName)
	output.PrintCmdStatus(message)
}

func copyFolderToLocal(zipFileSystem afero.Fs, localFileSystem afero.Fs, subDirectory string) error {
	dirInfo, err := afero.ReadDir(zipFileSystem, subDirectory)
	if err != nil {
		return err
	}
	for i := range dirInfo {
		zipLoc := subDirectory + "/" + dirInfo[i].Name()
		localLoc := convertZipLocToLocalLoc(subDirectory + "/" + dirInfo[i].Name())
		if !dirInfo[i].IsDir() {
			err = copyFile(zipFileSystem, localFileSystem, zipLoc, localLoc)
			if err != nil {
				return err
			}
		} else {
			err = localFileSystem.Mkdir(localLoc, os.ModeDir)
			if err != nil {
				return err
			}
			err = os.Chmod(localLoc, 0700)
			if err != nil {
				return err
			}
			err = copyFolderToLocal(zipFileSystem, localFileSystem, zipLoc)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func copyFile(zipFileSystem afero.Fs, localFileSystem afero.Fs, zipLoc string, localLoc string) error {
	data, err := afero.ReadFile(zipFileSystem, zipLoc)
	if err != nil {
		return err
	}
	_, err = localFileSystem.Create(localLoc)
	if err != nil {
		return err
	}
	err = afero.WriteFile(localFileSystem, localLoc, data, os.FileMode(os.O_RDWR))
	if err != nil {
		return err
	}
	return nil
}

func convertZipLocToLocalLoc(zipLoc string) string {
	secondSlashIndex := strings.Index(zipLoc[2:], "/")
	return zipLoc[secondSlashIndex+3:]
}
