package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/fivegreenapples/bound-variable/pkg/um"
)

func main() {

	programFile := flag.String("p", "", "program file")
	restoreFile := flag.String("r", "", "restore file")
	outputFile := flag.String("o", "", "output file")
	backupFolder := flag.String("b", "", "backup folder")
	flag.Parse()

	var err error
	var programFH, restoreFH, outputFH *os.File

	if *programFile != "" {
		programFH, err = os.Open(*programFile)
		if err != nil {
			fmt.Printf("error opening program file: %s\n", err)
			os.Exit(2)
		}
		defer programFH.Close()
	} else if *restoreFile != "" {
		restoreFH, err = os.Open(*restoreFile)
		if err != nil {
			fmt.Printf("error opening restore file: %s\n", err)
			os.Exit(2)
		}
		defer restoreFH.Close()
	} else {
		flag.Usage()
		os.Exit(1)
	}

	if *outputFile != "" {
		outputFH, err = os.Create(*outputFile)
		if err != nil {
			fmt.Printf("error opening output file: %s\n", err)
			os.Exit(2)
		}
		defer outputFH.Close()
	}

	if *backupFolder != "" {
		// check if the folder exists and is writable
		backupFH, errBU := os.Stat(*backupFolder)
		if errBU != nil {
			fmt.Printf("error stat-ing backup folder: %s\n", errBU)
			os.Exit(2)
		}
		if !backupFH.IsDir() {
			fmt.Printf("backup folder doesn't appear to be a folder: %s\n", *backupFolder)
			os.Exit(2)
		}
		*backupFolder, errBU = filepath.Abs(*backupFolder)
		if errBU != nil {
			fmt.Printf("error cleaning backup folder path: %s\n", errBU)
			os.Exit(2)
		}
	}

	myUM := um.New(os.Stdin, io.MultiWriter(os.Stdout, outputFH), os.Stderr, *backupFolder)
	if programFH != nil {
		err = myUM.LoadProgram(programFH)
		if err != nil {
			fmt.Printf("error loading program: %s\n", err)
			os.Exit(3)
		}
	} else {
		err = myUM.LoadFromBackup(restoreFH)
		if err != nil {
			fmt.Printf("error loading from backup: %s\n", err)
			os.Exit(3)
		}
	}

	endErr := myUM.Run()

	if endErr != nil {
		fmt.Println(endErr)
		os.Exit(4)
	}

}
