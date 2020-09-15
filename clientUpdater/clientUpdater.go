package main

import (
	"archive/zip"
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type ConfFile struct {
	MCVersion   string `json:"version"`
	MCDirectory string `json:"directory"`
}

func isWindows() bool {
	return os.PathSeparator == '\\' && os.PathListSeparator == ';'
}

// DownloadFile will download a url to a local file. It's efficient because it will
// write as it downloads and not load the whole file into memory.
func DownloadFile(filepath string, url string) error {

	// Get the data
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Create the file
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	// Write the body to file
	_, err = io.Copy(out, resp.Body)
	return err

}

// Unzip will decompress a zip archive, moving all files and folders
// within the zip file (parameter 1) to an output directory (parameter 2).
func Unzip(src string, dest string) ([]string, error) {

	var filenames []string

	r, err := zip.OpenReader(src)
	if err != nil {
		return filenames, err
	}
	defer r.Close()

	for _, f := range r.File {

		isModFile, err := regexp.MatchString("rxmc-Mods-master/[-._a-zA-Z0-9]*mods/.*\\.jar$", f.Name)
		if f.FileInfo().IsDir() || !isModFile || err != nil {
			continue
		}

		// Store filename/path for returning and using later on
		_, fileName := filepath.Split(f.Name)
		fpath := filepath.Join(dest, fileName)

		// Check for ZipSlip. More Info: http://bit.ly/2MsjAWE
		if !strings.HasPrefix(fpath, filepath.Clean(dest)+string(os.PathSeparator)) {
			return filenames, fmt.Errorf("%s: illegal file path", fpath)
		}

		filenames = append(filenames, fpath)

		// Make File
		if err = os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
			return filenames, err
		}

		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return filenames, err
		}

		rc, err := f.Open()
		if err != nil {
			return filenames, err
		}

		_, err = io.Copy(outFile, rc)

		// Close the file without defer to close before next iteration of loop
		outFile.Close()
		rc.Close()

		if err != nil {
			return filenames, err
		}
	}
	return filenames, nil
}

func SaveConfig(config ConfFile, jsonConfPath string) {
	newconfig, err := os.Open(jsonConfPath)
	if err != nil {
		newconfig, err = os.Create(jsonConfPath)
		if err != nil {
			panic(err)
		}
	}
	defer newconfig.Close()

	jsonData, err := json.Marshal(config)
	if err == nil {
		newconfig.Write(jsonData)
	} else {
		panic(err)
	}
}

func main() {
	bundledFabricInstaller := "fabric-installer-0.6.1.51.jar"
	fileURL := "https://github.com/rx13/rxmc-Mods/archive/master.zip"
	fileOut := "serverMods-master.zip"
	jsonConfPath := "clientUpdate.json"

	// set base module path for vanilla
	modPath := ""
	if isWindows() {
		modPath = path.Join(os.Getenv("APPDATA"), ".minecraft", "mods")
	} else {
		modPath = path.Join(os.Getenv("HOME"), ".minecraft", "mods")
	}

	// load and set config file if not present
	var config ConfFile
	configfile, err := os.Open(jsonConfPath)
	if err == nil {
		defer configfile.Close()
		filecontent, _ := ioutil.ReadAll(configfile)
		json.Unmarshal(filecontent, &config)
	} else {
		fmt.Println(err)
		// probably not present, assign new values
		config = ConfFile{MCVersion: "1.16.2", MCDirectory: modPath}
		SaveConfig(config, jsonConfPath)
	}

	// set common needs for module handling
	modPath = config.MCDirectory
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("Downloading lastest mods")
	err = DownloadFile(fileOut, fileURL)
	if err != nil {
		panic(err)
	}
	fmt.Println("> Downloaded: " + fileOut + "\n")

	// validate module path is intended
	fmt.Println("< Is this the correct minecraft MODS directory? (if not sure, just type yes) ")
	fmt.Print("  > " + modPath + " ? [y/n]: ")
	confirm, _ := reader.ReadString('\n')
	if strings.ToLower(confirm)[0] != byte('y') {
		fmt.Println("< Enter the correct path below")
		fmt.Print("  > ")
		newpath, _ := reader.ReadString('\n')
		newpath = strings.TrimSpace(newpath)
		if _, err := os.Stat(newpath); err == nil {
			modPath = newpath
			config.MCDirectory = newpath
			SaveConfig(config, jsonConfPath)
		} else {
			fmt.Printf("Location %s does not exist, exiting.\n", newpath)
			os.Exit(1)
		}
	}
	fmt.Println("")
	// mods path should end in "mods", else exit
	if !strings.HasSuffix(strings.ToLower(modPath), "mods") {
		fmt.Println("FATAL: the mod path should end in 'mods', but it is currently: " + modPath)
		fmt.Println("Exiting.")
		os.Exit(1)
	}

	// set minecraft relative paths
	minecraftPath := path.Dir(modPath)
	versionsPath := path.Join(minecraftPath, "versions")
	versions, err := ioutil.ReadDir(versionsPath)
	foundValidFabric := false
	if err != nil {
		fmt.Println("> No existing minecraft versions found.")
	} else {
		// check if minecraft version already exists with Fabric
		fmt.Println("Collecting existing version information.")
		for _, versionDirectory := range versions {
			if versionDirectory.IsDir() {
				dirName := path.Base(versionDirectory.Name())
				if strings.HasPrefix(dirName, "fabric-loader") && strings.HasSuffix(dirName, config.MCVersion) {
					foundValidFabric = true
				}
			}
		}
	}

	// if fabric isn't there, install it
	if !foundValidFabric {
		fmt.Println("> Installing designated Fabric + Minecraft version.")
		installFabric := exec.Command("java", "-jar", bundledFabricInstaller, "client", "-dir", minecraftPath, "-mcversion", config.MCVersion)
		err = installFabric.Run()
		if err != nil {
			fmt.Printf("Fabric Install Error: %s\n", err)
		} else {
			fmt.Println("> Install complete.")
		}
	} else {
		fmt.Println("> Fabric + Minecraft version already installed.")
	}

	if _, err := os.Stat(modPath); err == nil {
		fmt.Println("Removing old mods for Minecraft")
		err := os.RemoveAll(modPath)
		if err != nil {
			panic(err)
		}
		fmt.Println("> Mods have been removed")
	}
	os.MkdirAll(modPath, os.ModePerm)

	fmt.Println("Loading new mods for Minecraft")
	_, err = Unzip(fileOut, modPath)
	if err != nil {
		panic(err)
	}
	fmt.Println("> Mods loaded") // + strings.Join(extractedFiles, "\n  "))

	fmt.Println("Cleaning up")
	os.Remove(fileOut)
	fmt.Println("> Done")

	fmt.Printf("\n\n\n===== ADDITIONAL STEPS IF USING MultiMC =====\n\n")
	fmt.Printf("  1) Make sure the 'instance' version of minecraft is: %s\n", config.MCVersion)
	fmt.Printf("  2) Make sure the 'instance' version of FABRIC is up to date.\n    (%s is bundled with this)", bundledFabricInstaller)
	fmt.Printf("\n===== ===== ===== ===== ===== ===== ===== =====\n")

	i := 20
	fmt.Printf("Exiting in ")
	for {
		if i <= 0 {
			fmt.Println("0")
			break
		} else {
			fmt.Printf("%d.", i)
			time.Sleep(1 * time.Second)
			i--
		}
	}
}
