// Copyright (C) 2020 Markus L. Noga
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"runtime/debug"
	"strings"
	"time"
	nl "github.com/mlnoga/nightlight/internal"
	"github.com/pbnjay/memory"
	colorful "github.com/lucasb-eyer/go-colorful"
)

const version = "0.2.5"

type Job struct {
	Id       int
	FileName string
	Image    *nl.FITSImage 
	Err      error
}

var totalMiBs=memory.TotalMemory()/1024/1024

var cpuprofile = flag.String("cpuprofile", "", "write cpu profile to `file`")
var memprofile = flag.String("memprofile", "", "write memory profile to `file`")

var out  = flag.String("out", "out.fits", "save output to `file`")
var jpg  = flag.String("jpg", "%auto",  "save 8bit preview of output as JPEG to `file`. `%auto` replaces suffix of output file with .jpg")
var log  = flag.String("log", "%auto",    "save log output to `file`. `%auto` replaces suffix of output file with .log")
var pre  = flag.String("pre",  "",  "save pre-processed frames with given filename pattern, e.g. `pre%04d.fits`")
var stars= flag.String("stars","","save star detections with given filename pattern, e.g. `stars%04d.fits`")
var back = flag.String("back","","save extracted background with given filename pattern, e.g. `back%04d.fits`")
var post = flag.String("post", "",  "save post-processed frames with given filename pattern, e.g. `post%04d.fits`")
var batch= flag.String("batch", "", "save stacked batches with given filename pattern, e.g. `batch%04d.fits`")

var dark = flag.String("dark", "", "apply dark frame from `file`")
var flat = flag.String("flat", "", "apply flat frame from `file`")

var debayer = flag.String("debayer", "", "debayer the given channel, one of R, G, B or blank for no op")
var cfa     = flag.String("cfa", "RGGB", "color filter array type for debayering, one of RGGB, GRBG, GBRG, BGGR")

var binning= flag.Int64("binning", 0, "apply NxN binning, 0 or 1=no binning")

var bpSigLow  = flag.Float64("bpSigLow", 3.0,"low sigma for bad pixel removal as multiple of standard deviations")
var bpSigHigh = flag.Float64("bpSigHigh",5.0,"high sigma for bad pixel removal as multiple of standard deviations")

var starSig   = flag.Float64("starSig", 10.0,"sigma for star detection as multiple of standard deviations")
var starBpSig = flag.Float64("starBpSig",-1.0,"sigma for star detection bad pixel removal as multiple of standard deviations, -1: auto")
var starRadius= flag.Int64("starRadius", 16.0, "radius for star detection in pixels")

var backGrid  = flag.Int64("backGrid", 0, "automated background extraction: grid size in pixels, 0=off")
var backSigma = flag.Float64("backSigma", 1.5 ,"automated background extraction: sigma for detecting foreground objects")
var backClip  = flag.Int64("backClip", 0, "automated background extraction: clip the k brightest grid cells and replace with local median")

var usmSigma  = flag.Float64("usmSigma", 1, "unsharp masking sigma, ~1/3 radius")
var usmGain   = flag.Float64("usmGain", 0, "unsharp masking gain, 0=no op")
var usmThresh = flag.Float64("usmThresh", 1, "unsharp masking threshold, in standard deviations above background")

var align     = flag.Int64("align",1,"1=align frames, 0=do not align")
var alignK    = flag.Int64("alignK",20,"use triangles fromed from K brightest stars for initial alignment")
var alignT    = flag.Float64("alignT",1.0,"skip frames if alignment to reference frame has residual greater than this")

var lsEst     = flag.Int64("lsEst",3,"location and scale estimators 0=mean/stddev, 1=median/MAD, 2=IKSS, 3=iterative sigma-clipped sampled median and sampled Qn (standard)")
var normRange = flag.Int64("normRange",0,"normalize range: 1=normalize to [0,1], 0=do not normalize")
var normHist  = flag.Int64("normHist",3,"normalize histogram: 0=do not normalize, 1=location and scale, 2=black point shift for RGB align, 3=auto")

var stMode    = flag.Int64("stMode", 5, "stacking mode. 0=median, 1=mean, 2=sigma clip, 3=winsorized sigma clip, 4=linear fit, 5=auto")
var stClipPercLow = flag.Float64("stClipPercLow", 0.5,"set desired low clipping percentage for stacking, 0=ignore (overrides sigmas)")
var stClipPercHigh= flag.Float64("stClipPercHigh",0.5,"set desired high clipping percentage for stacking, 0=ignore (overrides sigmas)")
var stSigLow  = flag.Float64("stSigLow", -1,"low sigma for stacking as multiple of standard deviations, -1: use clipping percentage to find")
var stSigHigh = flag.Float64("stSigHigh",-1,"high sigma for stacking as multiple of standard deviations, -1: use clipping percentage to find")
var stWeight  = flag.Int64("stWeight", 0, "weights for stacking. 0=unweighted (default), 1=by exposure, 2=by inverse noise")
var stMemory  = flag.Int64("stMemory", int64((totalMiBs*7)/10), "total MiB of memory to use for stacking, default=0.7x physical memory")

var neutSigmaLow  = flag.Float64("neutSigmaLow", -1, "neutralize background color below this threshold, <0 = no op")
var neutSigmaHigh = flag.Float64("neutSigmaHigh", -1, "keep background color above this threshold, interpolate in between, <0 = no op")

var chromaGamma=flag.Float64("chromaGamma", 1.0, "scale LCH chroma curve by given gamma for luminances n sigma above background, 1.0=no op")
var chromaSigma=flag.Float64("chromaSigma", 1.0, "only scale and add to LCH chroma for luminances n sigma above background")

var chromaFrom= flag.Float64("chromaFrom", 295, "scale LCH chroma for hues in [from,to] by given factor, e.g. 295 to desaturate violet stars")
var chromaTo  = flag.Float64("chromaTo", 40, "scale LCH chroma for hues in [from,to] by given factor, e.g. 40 to desaturate violet stars")
var chromaBy  = flag.Float64("chromaBy", 1, "scale LCH chroma for hues in [from,to] by given factor, e.g. -1 to desaturate violet stars")

var rotFrom   = flag.Float64("rotFrom", 100, "rotate LCH color angles in [from,to] by given offset, e.g. 100 to aid Hubble palette for S2HaO3")
var rotTo     = flag.Float64("rotTo", 190, "rotate LCH color angles in [from,to] by given offset, e.g. 190 to aid Hubble palette for S2HaO3")
var rotBy     = flag.Float64("rotBy", 0, "rotate LCH color angles in [from,to] by given offset, e.g. -30 to aid Hubble palette for S2HaO3")

var scnr      = flag.Float64("scnr",0,"apply SCNR in [0,1] to green channel, e.g. 0.5 for tricolor with S2HaO3 and 0.1 for bicolor HaO3O3")

var autoLoc   = flag.Float64("autoLoc", 10, "histogram peak location in %% to target with automatic curves adjustment, 0=don't")
var autoScale = flag.Float64("autoScale", 0.4, "histogram peak scale in %% to target with automatic curves adjustment, 0=don't")

var midtone   = flag.Float64("midtone", 0, "midtone value in multiples of standard deviation; 0=no op")
var midBlack  = flag.Float64("midBlack", 2, "midtone black in multiples of standard deviation below background location")

var gamma     = flag.Float64("gamma", 1, "apply output gamma, 1: keep linear light data")
var ppGamma   = flag.Float64("ppGamma", 1, "apply post-peak gamma, scales curve from location+scale...ppLimit, 1: keep linear light data")
var ppSigma   = flag.Float64("ppSigma", 1, "apply post-peak gamma this amount of scales from the peak (to avoid scaling background noise)")

var scaleBlack= flag.Float64("scaleBlack", 0, "move black point so histogram peak location is given value in %%, 0=don't")

var darkF *nl.FITSImage=nil
var flatF *nl.FITSImage=nil

var lights   =[]*nl.FITSImage{}

func main() {
	debug.SetGCPercent(10)
	start:=time.Now()
	flag.Usage=func(){
 	    nl.LogPrintf(`Nightlight Copyright (c) 2020 Markus L. Noga
This program comes with ABSOLUTELY NO WARRANTY.
This is free software, and you are welcome to redistribute it under certain conditions.
Refer to https://www.gnu.org/licenses/gpl-3.0.en.html for details.

Usage: %s [-flag value] (stats|stack|rgb|argb|lrgb|legal) (img0.fits ... imgn.fits)

Commands:
  stats   Show input image statistics
  stack   Stack input images
  stretch Stretch single image
  rgb     Combine color channels. Inputs are treated as r, g and b channel in that order
  argb    Combine color channels and align with luminance. Inputs are treated as l, r, g and b channels
  lrgb    Combine color channels and combine with luminance. Inputs are treated as l, r, g and b channels
  legal   Show license and attribution information
  version Show version information

Flags:
`, os.Args[0])
	    flag.PrintDefaults()
	}
	flag.Parse()

	// Initialize logging to file in addition to stdout, if selected
	if *log=="%auto" {
		if *out!="" {
			*log=strings.TrimSuffix(*out, filepath.Ext(*out))+".log"			
		} else {
			*log=""
		}
	}
	if *log!="" { 
		err:=nl.LogAlsoToFile(*log)
		if err!=nil { nl.LogFatalf("Unable to open logfile '%s'\n", *log) }
	}

	// Also auto-select JPEG output target
	if *jpg=="%auto" {
		if *out!="" {
			*jpg=strings.TrimSuffix(*out, filepath.Ext(*out))+".jpg"			
		} else {
			*jpg=""
		}
	}

	// Enable CPU profiling if flagged
    if *cpuprofile != "" {
        f, err := os.Create(*cpuprofile)
        if err != nil {
            nl.LogFatal("Could not create CPU profile: ", err)
        }
        defer f.Close()
        if err := pprof.StartCPUProfile(f); err != nil {
            nl.LogFatal("Could not start CPU profile: ", err)
        }
      defer pprof.StopCPUProfile()
    }

    args:=flag.Args()
    if len(args)<1 {
    	flag.Usage()
    	return
    }
    if args[0]=="stats" || args[0]=="stack" || args[0]=="rgb" || args[0]=="argb" || args[0]=="lrgb" {
	    nl.LogPrintf("Using location and scale estimator %d\n", *lsEst)
		nl.LSEstimator=nl.LSEstimatorMode(*lsEst)
	}

    switch args[0] {
    case "stats":
    	cmdStats(args[1:])
    case "stack":
    	cmdStack(args[1:], *batch)
    case "stretch":
    	cmdStretch(args[1:])
    case "rgb":
    	cmdRGB(args[1:])
    case "argb":
    	cmdLRGB(args[1:],false)
    case "lrgb":
    	cmdLRGB(args[1:],true)
    case "legal":
    	cmdLegal()
    case "version":
    	nl.LogPrintf("Version %s\n", version)
    case "help", "?":
    	flag.Usage()
    default:
    	nl.LogPrintf("Unknown command '%s'\n\n", args[0])
    	flag.Usage()
    	return 
    }

	now:=time.Now()
	elapsed:=now.Sub(start)
	nl.LogPrintf("\nDone after %v\n", elapsed)

	// Store memory profile if flagged
    if *memprofile != "" {
        f, err := os.Create(*memprofile)
        if err != nil {
            nl.LogFatal("Could not create memory profile: ", err)
        }
        defer f.Close()
        runtime.GC() // get up-to-date statistics
        if err := pprof.Lookup("allocs").WriteTo(f,0); err != nil {
            nl.LogFatal("Could not write allocation profile: ", err)
        }
    }
    nl.LogSync()
}

// Perform optional preprocessing and statistics
func cmdStats(args []string) {
	// Set default parameters for this command
	if *normHist==nl.HNMAuto { *normHist=nl.HNMNone }
	if *starBpSig<0 { *starBpSig=5 } // default to noise elimination, we don't know if stats are called on single frame or resulting stack

    // Load dark and flat if flagged
    if *dark!="" { darkF=nl.LoadDark(*dark) }
    if *flat!="" { flatF=nl.LoadFlat(*flat) }
	if darkF!=nil && flatF!=nil && !nl.EqualInt32Slice(darkF.Naxisn, flatF.Naxisn) {
		nl.LogFatal("Error: flat and dark files differ in size")
	}

	// Glob file name wildcards
	fileNames:=globFilenameWildcards(args)

	// Preprocess light frames (subtract dark, divide flat, remove bad pixels, detect stars and HFR)
	nl.LogPrintf("\nPreprocessing %d frames with dark=%d flat=%d debayer=%s cfa=%s binning=%d normRange=%d bpSigLow=%.2f bpSigHigh=%.2f starSig=%.2f starBpSig=%.2f starRadius=%d backGrid=%d:\n", 
		len(fileNames), btoi(darkF!=nil), btoi(flatF!=nil), *debayer, *cfa, *binning, *normRange, *bpSigLow, *bpSigHigh, *starSig, *starBpSig, *starRadius, *backGrid)

	sem   :=make(chan bool, runtime.NumCPU())
	for id, fileName := range(fileNames) {
		sem <- true 
		go func(id int, fileName string) {
			defer func() { <-sem }()
			lightP, err:=nl.PreProcessLight(id, fileName, darkF, flatF, *debayer, *cfa, int32(*binning), int32(*normRange), float32(*bpSigLow), float32(*bpSigHigh), float32(*starSig), float32(*starBpSig), int32(*starRadius), int32(*backGrid), float32(*backSigma), int32(*backClip), *back)
			if err!=nil {
				nl.LogPrintf("%d: Error: %s\n", id, err.Error())
			} else {
				if (*pre)!="" {
					err=lightP.WriteFile(fmt.Sprintf((*pre), id))
					if err!=nil { nl.LogFatalf("Error writing file: %s\n", err) }
				}
				if (*stars)!="" {
					starsFits:=nl.ShowStars(lightP, 2.0)
					err=starsFits.WriteFile(fmt.Sprintf((*stars), id))
					if err!=nil { nl.LogFatalf("Error writing file: %s\n", err) }
					starsFits.Data=nil
				}
				lightP.Data=nil
			}
		}(id, fileName)
	}
	for i:=0; i<cap(sem); i++ {  // wait for goroutines to finish
		sem <- true
	}
}


// Perform stacking command
func cmdStack(args []string, batchPattern string) {
	// Set default parameters for this command
	if *normHist==nl.HNMAuto { *normHist=nl.HNMLocScale }
	if *starBpSig<0 { *starBpSig=5 } // default to noise elimination when working with individual subexposures

	// The stack of stacks
	var stack *nl.FITSImage = nil
	var stackFrames int64 = 0
	var stackNoise  float32 = 0

    // Load dark and flat in parallel if flagged
    sem   :=make(chan bool, 2) // limit parallelism to 2
    if *dark!="" { 
		go func() { 
			darkF=nl.LoadDark(*dark) 
			sem <- true
		}() 
	}
    if *flat!="" { 
    	go func() { 
    		flatF=nl.LoadFlat(*flat) 
			sem <- true
		}() 
	}
    if *dark!="" {   // wait for goroutine to finish
		<- sem
	}
    if *flat!="" {   // wait for goroutine to finish
		<- sem
	}

	if darkF!=nil && flatF!=nil && !nl.EqualInt32Slice(darkF.Naxisn, flatF.Naxisn) {
		nl.LogFatal("Error: flat and dark files differ in size")
	}

	// Glob file name wildcards
	fileNames:=globFilenameWildcards(args)
	if fileNames==nil || len(fileNames)==0 {
		nl.LogFatal("Error: no input files")
	}
	// Split input into required number of randomized batches, given the permissible amount of memory
	numBatches, batchSize, overallIDs, overallFileNames, imageLevelParallelism:=nl.PrepareBatches(fileNames, *stMemory, darkF, flatF)

	// Process each batch. The first batch sets the reference image, and if solving for sigLow/High also those. 
	// They are then reused in subsequent batches
	refFrame:=(*nl.FITSImage)(nil)
	sigLow, sigHigh:=float32(-1), float32(-1)
	for b:=int64(0); b<numBatches; b++ {
		// Cut out relevant part of the overall input filenames
		batchStartOffset:= b   *batchSize
		batchEndOffset  :=(b+1)*batchSize
		if batchEndOffset>int64(len(fileNames)) { batchEndOffset=int64(len(fileNames)) }
		batchFrames     :=batchEndOffset-batchStartOffset
		ids      :=overallIDs      [batchStartOffset:batchEndOffset]
		fileNames:=overallFileNames[batchStartOffset:batchEndOffset]
		nl.LogPrintf("\nStarting batch %d of %d with %d images: %v...\n", b, numBatches, len(ids), ids)

		// Stack the files in this batch
		batch, avgNoise :=(*nl.FITSImage)(nil), float32(0)
		batch, refFrame, sigLow, sigHigh, avgNoise=stackBatch(ids, fileNames, refFrame, sigLow, sigHigh, imageLevelParallelism)

		// Find stars in the newly stacked batch and report out on them
		batch.Stars, _, batch.HFR=nl.FindStars(batch.Data, batch.Naxisn[0], batch.Stats.Location, batch.Stats.Scale, 
			float32(*starSig), float32(*starBpSig), int32(*starRadius), nil)
		nl.LogPrintf("Batch %d stack: Stars %d HFR %.2f Exposure %gs %v\n", b, len(batch.Stars), batch.HFR, batch.Exposure, batch.Stats)

		expectedNoise:=avgNoise/float32(math.Sqrt(float64(batchFrames)))
		nl.LogPrintf("Batch %d expected noise %.4g from stacking %d frames with average noise %.4g\n",
					b, expectedNoise, int(batchFrames), avgNoise )

		// Save batch if desired
		if batchPattern!="" {
			batchFileName:=fmt.Sprintf(batchPattern, b)
			nl.LogPrintf("Writing batch result to %s\n", batchFileName)
			err:=batch.WriteFile(batchFileName)
			if err!=nil { nl.LogFatalf("Error writing file: %s\n", err) }
		}

		// Update stack of stacks
		if numBatches>1 {
			stack=nl.StackIncremental(stack, batch, float32(batchFrames))
			stackFrames+=batchFrames
			stackNoise +=batch.Stats.Noise*float32(batchFrames)
		} else {
			stack=batch
		}

		// Free memory
		ids, fileNames, batch=nil, nil, nil
		debug.FreeOSMemory()
	}

	// Free more memory
	refFrame=nil  // all other primary frames already freed after stacking
	if darkF!=nil { darkF=nil }
	if flatF!=nil { flatF=nil }
	debug.FreeOSMemory()

	if numBatches>1 {
		// Finalize stack of stacks
		err:=nl.StackIncrementalFinalize(stack, float32(stackFrames))
		if err!=nil { nl.LogPrintf("Error calculating extended stats: %s\n", err) }

		// Find stars in newly stacked image and report out on them
		stack.Stars, _, stack.HFR=nl.FindStars(stack.Data, stack.Naxisn[0], stack.Stats.Location, stack.Stats.Scale, 
			float32(*starSig), float32(*starBpSig), int32(*starRadius), nil)
		nl.LogPrintf("Overall stack: Stars %d HFR %.2f Exposure %gs %v\n", len(stack.Stars), stack.HFR, stack.Exposure, stack.Stats)

		avgNoise:=stackNoise/float32(stackFrames)
		expectedNoise:=avgNoise/float32(math.Sqrt(float64(numBatches)))
		nl.LogPrintf("Expected noise %.4g from stacking %d batches with average noise %.4g\n",
					expectedNoise, int(numBatches), avgNoise )
	}

	// Apply output gamma if desired
	if (*gamma)!=1 {
		nl.LogPrintf("Applying gamma %.3g\n", *gamma)
		stack.ApplyGamma(float32(*gamma))
	}

    // write out results, then free memory for the overall stack
	err:=stack.WriteFile(*out)
	if err!=nil { nl.LogFatalf("Error writing file: %s\n", err) }
	stack=nil
}

// Stack a given batch of files, using the reference provided, or selecting a reference frame if nil.
// Returns the stack for the batch, and the reference frame
func stackBatch(ids []int, fileNames []string, refFrame *nl.FITSImage, sigLow, sigHigh float32, imageLevelParallelism int32) (stack, refFrameOut *nl.FITSImage, sigLowOut, sigHighOut, avgNoise float32) {
	// Preprocess light frames (subtract dark, divide flat, remove bad pixels, detect stars and HFR)
	nl.LogPrintf("\nPreprocessing %d frames with dark=%d flat=%d debayer=%s cfa=%s binning=%d normRange=%d bpSigLow=%.2f bpSigHigh=%.2f starSig=%.2f starBpSig=%.2f starRadius=%d backGrid=%d:\n", 
		len(fileNames), btoi(darkF!=nil), btoi(flatF!=nil), *debayer, *cfa, *binning, *normRange, *bpSigLow, *bpSigHigh, *starSig, *starBpSig, *starRadius, *backGrid)
	lights:=nl.PreProcessLights(ids, fileNames, darkF, flatF, *debayer, *cfa, int32(*binning), int32(*normRange), float32(*bpSigLow), float32(*bpSigHigh), 
		float32(*starSig), float32(*starBpSig), int32(*starRadius), *stars, int32(*backGrid), float32(*backSigma), int32(*backClip), *back, *pre, imageLevelParallelism)
	debug.FreeOSMemory()					

	// Remove nils from lights, in case of read errors
	o:=0
	for i:=0; i<len(lights); i+=1 {
		if lights[i]!=nil {
			lights[o]=lights[i]
			o+=1
		}
	}
	lights=lights[:o]

	avgNoise=float32(0)
	for _,l:=range lights {
		avgNoise+=l.Stats.Noise
	}
	avgNoise/=float32(len(lights))
	nl.LogPrintf("Average input frame noise is %.4g\n", avgNoise)

	// Select reference frame, unless one was provided from prior batches
	if (*align!=0 || *normHist!=0) && (refFrame==nil) {
		refFrameScore:=float32(0)
		refFrame, refFrameScore=nl.SelectReferenceFrame(lights)
		if refFrame==nil { panic("Reference frame for alignment and normalization not found.") }
		nl.LogPrintf("Using frame %d as reference. Score %.4g, %v.\n", refFrame.ID, refFrameScore, refFrame.Stats)
	}

	// Post-process all light frames (align, normalize)
	nl.LogPrintf("\nPostprocessing %d frames with align=%d alignK=%d alignT=%.3f normHist=%d usmSigma=%g usmGain=%g usmThresh=%g:\n", 
		         len(lights), *align, *alignK, *alignT, *normHist, float32(*usmSigma), float32(*usmGain), float32(*usmThresh))
	nl.PostProcessLights(refFrame, refFrame, lights, int32(*align), int32(*alignK), float32(*alignT), nl.HistoNormMode(*normHist), nl.OOBModeNaN, 
	                     float32(*usmSigma), float32(*usmGain), float32(*usmThresh), *post, imageLevelParallelism)
	debug.FreeOSMemory()					

	// Remove nils from lights again, in case of alignment errors
	o=0
	for i:=0; i<len(lights); i+=1 {
		if lights[i]!=nil {
			lights[o]=lights[i]
			o+=1
		}
	}
	lights=lights[:o]

	// Prepare weights for stacking, using 1/noise. 
	weights:=[]float32(nil)
	if (*stWeight)==1 { // exposure weighted stacking
		weights =make([]float32, len(lights))
		for i:=0; i<len(lights); i+=1 {
			if lights[i].Exposure==0 { nl.LogFatalf("%d: Missing exposure information for exposure-weighted stacking", lights[i].ID) }
			weights[i]=lights[i].Exposure
		}
	} else if (*stWeight)==2 { // noise weighted stacking
		minNoise, maxNoise:=float32(math.MaxFloat32), float32(-math.MaxFloat32)
		for i:=0; i<len(lights); i+=1 {
			n:=lights[i].Stats.Noise
			if n<minNoise { minNoise=n }
			if n>maxNoise { maxNoise=n }
		}		
		weights =make([]float32, len(lights))
		for i:=0; i<len(lights); i+=1 {
			lights[i].Stats.Noise=nl.EstimateNoise(lights[i].Data, lights[i].Naxisn[0])
			weights[i]=1/(1+4*(lights[i].Stats.Noise-minNoise)/(maxNoise-minNoise))
		}
	}

	refFrameLoc:=float32(0)
	if refFrame!=nil && refFrame.Stats!=nil {
		refFrameLoc=refFrame.Stats.Location
	}

	// Stack the post-processed lights 
	if sigLow>=0 && sigHigh>=0 {
		// Use sigma bounds from prior batch for stacking
		nl.LogPrintf("\nStacking %d frames with mode %d stWeight %d and sigLow %.2f sigHigh %.2f from prior batch\n", len(lights), *stMode, *stWeight, sigLow, sigHigh)
		var err error
		stack, _, _, err=nl.Stack(lights, nl.StackMode(*stMode), weights, refFrameLoc, sigLow, sigHigh)
		if err!=nil { nl.LogFatal(err.Error()) }
	} else if *stSigLow>=0 && *stSigHigh>=0 {
		// Use given sigma bounds for stacking
		nl.LogPrintf("\nStacking %d frames with mode %d stWeight %d stSigLow %.2f stSigHigh %.2f\n", len(lights), *stMode, *stWeight, *stSigLow, *stSigHigh)
		var err error
		stack, _, _, err=nl.Stack(lights, nl.StackMode(*stMode), weights, refFrameLoc, float32(*stSigLow), float32(*stSigHigh))
		if err!=nil { nl.LogFatal(err.Error()) }
	} else {
		// Find sigma bounds based on desired clipping percentages
		nl.LogPrintf("\nFinding sigmas for stacking %d frames into %s with mode %d stWeight %d to achieve stClipLow/high %.2f%%/%.2f%%\n", len(lights), *out, *stMode, *stWeight, *stClipPercLow, *stClipPercHigh )
		var err error
		stack, _, _, sigLow, sigHigh, err=nl.FindSigmasAndStack(lights, nl.StackMode(*stMode), weights, refFrameLoc, float32(*stClipPercLow), float32(*stClipPercHigh))
		if err!=nil { nl.LogFatal(err.Error()) }
	}

	// Free memory
	lights=nil
	debug.FreeOSMemory()

	return stack, refFrame, sigLow, sigHigh, avgNoise
}


func cmdStretch(args []string) {
	fileNames:=globFilenameWildcards(args)
	if len(fileNames)!=1 {
		nl.LogFatal("Need exactly one file to perform a stretch")
	}

	f:=nl.NewFITSImage()
	f.ID=0
	err:=f.ReadFile(fileNames[0])
	if err!=nil { 
		nl.LogFatalf("Error reading FITS file %s", fileNames[0])
	}
	f.Stats, err=nl.CalcExtendedStats(f.Data, f.Naxisn[0])
	if err!=nil { 
		nl.LogFatalf("%d: Calculating stats: %s", f.ID, err) 
	}


	nl.Stretch(&f, float32(*autoLoc), float32(*autoScale), float32(*midtone), float32(*midBlack), 
	               float32(*gamma),   float32(*ppGamma),   float32(*ppSigma), float32(*scaleBlack) )

    // write out results, then free memory for the overall stack
	nl.LogPrintf("Writing FITS to %s ...\n", *out)
	err=f.WriteFile(*out)
	if err!=nil { nl.LogFatalf("Error writing file: %s\n", err) }
	if (*jpg)!="" {
		nl.LogPrintf("Writing JPG to %s ...\n", *jpg)
		f.WriteMonoJPGToFile(*jpg, 95)
		if err!=nil { nl.LogFatalf("Error writing file: %s\n", err) }
	}
}


// Perform RGB combination command
func cmdRGB(args []string) {
	// Set default parameters for this command
	if *normHist==nl.HNMAuto { *normHist=nl.HNMNone }
	if *starBpSig<0 { *starBpSig=0 }  // inputs are typically stacked and have undergone noise removal

	// Glob file name wildcards
	fileNames:=globFilenameWildcards(args)
	if len(fileNames)!=3 {
		nl.LogFatal("Need exactly three input files to perform a RGB combination")
	}
	ids:=[]int{0,1,2}

	// Read files and detect stars
	imageLevelParallelism:=int32(runtime.GOMAXPROCS(0))
	if imageLevelParallelism>3 { imageLevelParallelism=3 }
	nl.LogPrintf("\nReading color channels and detecting stars:\n")
	lights:=nl.PreProcessLights(ids, fileNames, nil, nil, *debayer, *cfa, int32(*binning), 1, 0, 0, 
		float32(*starSig), float32(*starBpSig), int32(*starRadius), *stars, int32(*backGrid), float32(*backSigma), int32(*backClip), *back, *pre, imageLevelParallelism)

	// Pick reference frame
	var refFrame *nl.FITSImage
	var refFrameScore float32

	if (*align)!=0 || (*normHist)!=0 {
		refFrame, refFrameScore=nl.SelectReferenceFrame(lights)
		if refFrame==nil { panic("Reference channel for alignment not found.") }
		nl.LogPrintf("Using channel %d with score %.4g as reference for alignment and normalization.\n\n", refFrame.ID, refFrameScore)
	}

	// Post-process all channels (align, normalize)
	var oobMode nl.OutOfBoundsMode=nl.OOBModeOwnLocation
	nl.LogPrintf("Postprocessing %d channels with align=%d alignK=%d alignT=%.3f normHist=%d oobMode=%d usmSigma=%g usmGain=%g usmThresh=%g:\n", 
				 len(lights), *align, *alignK, *alignT, *normHist, oobMode, float32(*usmSigma), float32(*usmGain), float32(*usmThresh))
	numErrors:=nl.PostProcessLights(refFrame, refFrame, lights, int32(*align), int32(*alignK), float32(*alignT), nl.HistoNormMode(*normHist), oobMode, 
									float32(*usmSigma), float32(*usmGain), float32(*usmThresh), *post, imageLevelParallelism)
    if numErrors>0 { nl.LogFatal("Need aligned RGB frames to proceed") }

	// Combine RGB channels
	nl.LogPrintf("\nCombining color channels...\n")
	rgb:=nl.CombineRGB(lights, refFrame)

	postProcessAndSaveRGBComposite(&rgb, nil)
	rgb.Data=nil
}


// Perform LRGB combination command
func cmdLRGB(args []string, applyLuminance bool) {
	// Set default parameters for this command
	if *normHist==nl.HNMAuto { *normHist=nl.HNMNone }
	if *starBpSig<0 { *starBpSig=0 }    // inputs are typically stacked and have undergone noise removal

	// Glob file name wildcards
	fileNames:=globFilenameWildcards(args)
	if len(fileNames)!=4 {
		nl.LogFatal("Need exactly four input files to perform a LRGB combination")
	}
	ids:=[]int{0,1,2,3}

	// Read files and detect stars
	imageLevelParallelism:=int32(runtime.GOMAXPROCS(0))
	if imageLevelParallelism>4 { imageLevelParallelism=4 }
	nl.LogPrintf("\nReading color channels and detecting stars:\n")
	lights:=nl.PreProcessLights(ids, fileNames, nil, nil, *debayer, *cfa, int32(*binning), 1, 0, 0, 
		float32(*starSig), float32(*starBpSig), int32(*starRadius), *stars, int32(*backGrid), float32(*backSigma), int32(*backClip), *back, *pre, imageLevelParallelism)

	var refFrame, histoRef *nl.FITSImage
	if (*align)!=0 {
		// Always use luminance as reference frame
		refFrame=lights[0]
		nl.LogPrintf("Using luminance channel %d as reference for alignment.\n", refFrame.ID)
	}

	if (*normHist)!=0 {
		// Normalize to [0,1]
		histoRef=lights[1]
		minLoc:=float32(histoRef.Stats.Location)
	    for id, light:=range(lights) {
	    	if id>0 && light.Stats.Location<minLoc { 
	    		minLoc=light.Stats.Location 
	    		histoRef=light
	    	}
	    }
		nl.LogPrintf("Using color channel %d as reference for RGB peak normalization to %.4g...\n\n", histoRef.ID, histoRef.Stats.Location)
	}

	// Align images if selected
	var oobMode nl.OutOfBoundsMode=nl.OOBModeOwnLocation
	nl.LogPrintf("Postprocessing %d channels with align=%d alignK=%d alignT=%.3f normHist=%d oobMode=%d usmSigma=%g usmGain=%g usmThresh=%g:\n", 
		         len(lights), *align, *alignK, *alignT, *normHist, oobMode, *usmSigma, *usmGain, *usmThresh)
	numErrors:=nl.PostProcessLights(refFrame, histoRef, lights, int32(*align), int32(*alignK), float32(*alignT), nl.HistoNormMode(*normHist), oobMode, 
									float32(*usmSigma), float32(*usmGain), float32(*usmThresh), "", imageLevelParallelism)
    if numErrors>0 { nl.LogFatal("Need aligned RGB frames to proceed") }

	// Combine RGB channels
	nl.LogPrintf("\nCombining color channels...\n")
	rgb:=nl.CombineRGB(lights[1:], lights[0])

	if applyLuminance {
		postProcessAndSaveRGBComposite(&rgb, lights[0])
	} else {
		postProcessAndSaveRGBComposite(&rgb, nil)
	}
	rgb.Data=nil
}

func postProcessAndSaveRGBComposite(rgb *nl.FITSImage, lum *nl.FITSImage) {
	// Auto-balance colors in linear RGB color space
	autoBalanceColors(rgb)

	nl.LogPrintln("Converting color image to HSLuv color space")
	rgb.RGBToHSLuv()

	// Apply LRGB combination in linear CIE xyY color space
	if lum!=nil {
		// nl.LogPrintln("Converting linear RGB to linear CIE xyY for LRGB combination")
	    // rgb.ToXyy()

	    nl.LogPrintln("Converting luminance image to HSLuv as well...")
	    lum.MonoToLum()

		nl.LogPrintln("Applying luminance image to luminance channel...")
		rgb.ApplyLuminanceToCIExyY(lum)

		// nl.LogPrintln("Converting linear CIE xyY to linear RGB")
		// rgb.XyyToRGB()
	}

    if (*neutSigmaLow>=0) && (*neutSigmaHigh>=0) {
		nl.LogPrintf("Neutralizing background values below %.4g sigma, keeping color above %.4g sigma\n", *neutSigmaLow, *neutSigmaHigh)    	

		loc, scale, err:=nl.HCLLumLocScale(rgb.Data, rgb.Naxisn[0])
		if err!=nil { nl.LogFatal(err) }
		low :=loc + scale*float32(*neutSigmaLow)
		high:=loc + scale*float32(*neutSigmaHigh)
		nl.LogPrintf("Location %.2f%%, scale %.2f%%, low %.2f%% high %.2f%%\n", loc*100, scale*100, low*100, high*100)

		rgb.NeutralizeBackground(low, high)		
    }

    if (*chromaGamma)!=1 {
    	nl.LogPrintf("Applying gamma %.2f to saturation for values %.4g sigma above background...\n", *chromaGamma, *chromaSigma)

		// calculate basic image stats as a fast location and scale estimate
		loc, scale, err:=nl.HCLLumLocScale(rgb.Data, rgb.Naxisn[0])
		if err!=nil { nl.LogFatal(err) }
		threshold :=loc + scale*float32(*chromaSigma)
		nl.LogPrintf("Location %.2f%%, scale %.2f%%, threshold %.2f%%\n", loc*100, scale*100, threshold*100)

		rgb.AdjustChroma(float32(*chromaGamma), threshold)
    }

    if (*chromaBy)!=1 {
    	nl.LogPrintf("Multiplying LCH chroma (saturation) by %.4g for hues in [%g,%g]...\n", *chromaBy, *chromaFrom, *chromaTo)
		rgb.AdjustChromaForHues(float32(*chromaFrom), float32(*chromaTo), float32(*chromaBy))
    }

    if (*rotBy)!=0 {
    	nl.LogPrintf("Rotating LCH hue angles in [%g,%g] by %.4g...\n", *rotFrom, *rotTo, *rotBy)
		rgb.RotateColors(float32(*rotFrom), float32(*rotTo), float32(*rotBy))
    }

    if (*scnr)!=0 {
    	nl.LogPrintf("Applying SCNR of %.4g ...\n", *scnr)
		rgb.SCNR(float32(*scnr))
    }

	// Optionally adjust midtones
	if (*midtone)!=0 {
		nl.LogPrintf("Applying midtone correction with midtone=%.2f%% x scale and black=location - %.2f%% x scale\n", *midtone, *midBlack)
		// calculate basic image stats as a fast location and scale estimate
		loc, scale, err:=nl.HCLLumLocScale(rgb.Data, rgb.Naxisn[0])
		if err!=nil { nl.LogFatal(err) }
		absMid:=float32(*midtone)*scale
		absBlack:=loc - float32(*midBlack)*scale
		nl.LogPrintf("loc %.2f%% scale %.2f%% absMid %.2f%% absBlack %.2f%%\n", 100*loc, 100*scale, 100*absMid, 100*absBlack)
		rgb.ApplyMidtonesToChannel(2, absMid, absBlack)
	}

	// Optionally adjust gamma
	if (*gamma)!=1 {
		nl.LogPrintf("Applying gamma %.3g\n", *gamma)
		rgb.ApplyGammaToChannel(2, float32(*gamma))
	}

	// Optionally adjust gamma post peak
	if (*ppGamma)!=1 {
	         loc, scale, err:=nl.HCLLumLocScale(rgb.Data, rgb.Naxisn[0])
	         if err!=nil { nl.LogFatal(err) }
	 from:=loc+float32(*ppSigma)*scale
	 to  :=float32(1.0)
	 nl.LogPrintf("Based on sigma=%.4g, boosting values in [%.2f%%, %.2f%%] with gamma %.4g...\n", *ppSigma, from*100, to*100, *ppGamma)
	         rgb.ApplyPartialGammaToChannel(2, from, to, float32(*ppGamma))
	}

	// Optionally scale histogram peak
	if (*scaleBlack)!=0 {
	 xyyTargetBlack:=float32((*scaleBlack)/100.0)
		_,_,hclTargetBlack:=colorful.Xyy(0,0,float64(xyyTargetBlack)).Hcl()
		targetBlack:=float32(hclTargetBlack)
		loc, scale, err:=nl.HCLLumLocScale(rgb.Data, rgb.Naxisn[0])
		if err!=nil { nl.LogFatal(err) }
		nl.LogPrintf("Location %.2f%% and scale %.2f%%: ", loc*100, scale*100)
		if loc>targetBlack {
			nl.LogPrintf("scaling black to move location to HCL %.2f%% for linear %.2f%%...\n", targetBlack*100.0, xyyTargetBlack*100.0)
			rgb.ShiftBlackToMoveChannel(2,loc, targetBlack)
		} else {
			nl.LogPrintf("cannot move to location %.2f%% by scaling black\n", targetBlack*100.0)
		}
	}

	nl.LogPrintln("Converting nonlinear HSLuv to linear RGB")
    rgb.HSLuvToRGB()

	// Write outputs
	nl.LogPrintf("Writing FITS to %s ...\n", *out)
	err:=rgb.WriteFile(*out)
	if err!=nil { nl.LogFatalf("Error writing file: %s\n", err) }
	if (*jpg)!="" {
		nl.LogPrintf("Writing JPG to %s ...\n", *jpg)
		rgb.WriteJPGToFile(*jpg, 95)
		if err!=nil { nl.LogFatalf("Error writing file: %s\n", err) }
	}
}


// Automatically balance colors with multiple iterations of SetBlackWhitePoints, producing log output
func autoBalanceColors(rgb *nl.FITSImage) {
	if len(rgb.Stars)==0 {
		nl.LogPrintln("Skipping black and white point adjustment as zero stars have been detected")
	} else {
		nl.LogPrintln("Setting black point so histogram peaks align and white point so median star color becomes neutral...")
		for i:=0; i<3; i++ {
			err:=rgb.SetBlackWhitePoints()
			if err!=nil { nl.LogFatal(err) }
		}
	}
}


// Turn filename wildcards into list of light frame files
func globFilenameWildcards(args []string) []string {
	if len(args)<1 { nl.LogFatal("No frames to process.") }
	fileNames:=[]string{}
	for _, pattern := range args {
		matches, err := filepath.Glob(pattern)
		if err!=nil { nl.LogFatal(err) }
		fileNames=append(fileNames, matches...)
	}
	nl.LogPrintf("Found %d frames:\n", len(fileNames))
	for i, fileName :=range fileNames {
		nl.LogPrintf("%d:%s\n",i, fileName)
	}
	return fileNames
}

// Helper: convert bool to int
func btoi(b bool) int {
	if b { return 1 }
	return 0
}

// Show licensing information
func cmdLegal() {
	nl.LogPrint(`Nightlight is Copyright (c) 2020 Markus L. Noga
This program comes with ABSOLUTELY NO WARRANTY.
This is free software, and you are welcome to redistribute it under certain conditions.
Refer to https://www.gnu.org/licenses/gpl-3.0.en.html for details.
The binary version of this program uses several open source libraries and components, which come with their own licensing terms. See below for a list of attributions.

ATTRIBUTIONS

A1. https://github.com/gonum/gonum is Copyright (c) 2013 The Gonum Authors. All rights reserved.

Redistribution and use in source and binary forms, with or without modification, are permitted provided that the following conditions are met:

* Redistributions of source code must retain the above copyright notice, this list of conditions and the following disclaimer.

* Redistributions in binary form must reproduce the above copyright notice, this list of conditions and the following disclaimer in the documentation and/or other materials provided with the distribution.

* Neither the name of the copyright holder nor the names of its contributors may be used to endorse or promote products derived from this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.


A2. https://github.com/pbnjay/memory is Copyright (c) 2017, Jeremy Jay. All rights reserved.

Redistribution and use in source and binary forms, with or without modification, are permitted provided that the following conditions are met:

* Redistributions of source code must retain the above copyright notice, this list of conditions and the following disclaimer.

* Redistributions in binary form must reproduce the above copyright notice, this list of conditions and the following disclaimer in the documentation and/or other materials provided with the distribution.

* Neither the name of the copyright holder nor the names of its contributors may be used to endorse or promote products derived from this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.


A3. https://github.com/valyala/fastrand is Copyright (c) 2017 Aliaksandr Valialkin

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"), to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.


A4. https://github.com/lucasb-eyer/go-colorful is Copyright (c) 2013 Lucas Beyer

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"), to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.


A5. https://github.com/klauspost/cpuid is Copyright (c) 2015 Klaus Post

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"), to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
`)
}