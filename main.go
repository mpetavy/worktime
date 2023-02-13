package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mpetavy/common"
	"github.com/wlbr/feiertage"
)

const mask = common.Day + common.DateSeparator + common.Month + common.DateSeparator + common.Year + common.Separator + common.Hour + common.TimeSeparator + common.Minute

const (
	Gleittag = "#Gleittag"
	Urlaub   = "#Urlaub"
)

var (
	filename         *string
	minutes          *bool
	vacationPerMonth *float64
	export           *string
	limit            *string

	listFeiertage []feiertage.Feiertag
	yearFeiertage int
	limitDate     time.Time
)

type Day struct {
	start   time.Time
	end     time.Time
	comment string
}

func init() {
	common.Init("worktime", "1.0.27", "", "", "2017", "tracks your working times", "mpetavy", fmt.Sprintf("https://github.com/mpetavy/%s", common.Title()), common.APACHE, nil, nil, nil, run, time.Minute)

	minutes = flag.Bool("m", false, "show durations in minutes")
	vacationPerMonth = flag.Float64("v", 2.916, "vacation per month")

	fn := ""

	usr, err := user.Current()
	if common.Error(err) {
		panic(err)
	}

	if common.IsRunningInteractive() {
		fn = filepath.Join(usr.HomeDir, string(os.PathSeparator), "Documents", string(os.PathSeparator), "worktime", string(os.PathSeparator), "worktime.csv")
	}

	filename = flag.String("f", fn, "filename for worktime.csv")
	export = flag.String("e", "", "filename for export worktime.csv")
	limit = flag.String("l", "", "date until summary should be processed")

	common.Events.NewFuncReceiver(common.EventFlagsParsed{}, func(event common.Event) {
		if *limit != "" {
			limitDate, err = common.ParseDateTime(common.DateMask, *limit)
			if common.Error(err) {
				panic(err)
			}
		}
	})
}

func findDay(lines *[]Day, date time.Time) (*Day, bool) {
	for i := 0; i < len(*lines); i++ {
		if common.CompareDate(date, (*lines)[i].start) == 0 {
			return &(*lines)[i], true
		}
	}

	return &Day{}, false
}

func readWorktimes(filename string, start *time.Time, end *time.Time, lines *[]Day) error {
	file, err := os.Open(filename)
	if common.Error(err) {
		return err
	}

	defer func() {
		common.DebugError(file.Close())
	}()

	r := csv.NewReader(file)

	r.Comma = ';'
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	r.TrimLeadingSpace = true

	for {
		record, err := r.Read()
		if err == io.EOF {
			err = nil
			break
		}
		if common.Error(err) {
			return err
		}

		txt := record[0]
		p := strings.Index(txt, ",")

		if p != -1 {
			txt = txt[0:p]
		}

		date, err := common.ParseDateTime(mask, txt)
		if common.Error(err) {
			return err
		}

		if !limitDate.IsZero() && common.CompareDate(date, limitDate) == 0 {
			break
		}

		if common.CompareDate(date, time.Now()) == 0 {
			if start.IsZero() {
				*start = date
			} else {
				*end = date
			}
		} else {
			day, found := findDay(lines, date)

			if found {
				day.end = date
			} else {
				var comment string

				if len(record) >= 4 {
					if strings.HasPrefix(record[3], "#") || strings.HasPrefix(record[3], "?") {
						comment = record[3]
					}
				}

				*lines = append(*lines, Day{date, date, comment})
			}
		}
	}

	return err
}

func createFeiertage(year int) {
	if year != yearFeiertage {
		listFeiertage = []feiertage.Feiertag{
			feiertage.Neujahr(year),
			feiertage.HeiligeDreiKönige(year),
			feiertage.Karfreitag(year),
			feiertage.Ostermontag(year),
			feiertage.TagDerArbeit(year),
			feiertage.ChristiHimmelfahrt(year),
			feiertage.Pfingstmontag(year),
			feiertage.Fronleichnam(year),
			feiertage.MariäHimmelfahrt(year),
			feiertage.TagDerDeutschenEinheit(year),
			feiertage.Allerheiligen(year),
			feiertage.Heiligabend(year),
			feiertage.Weihnachten(year),
			feiertage.ZweiterWeihnachtsfeiertag(year),
		}

		yearFeiertage = year
	}
}

func getFeiertag(tag time.Time) string {
	createFeiertage(tag.Year())

	for _, e := range listFeiertage {
		day := common.ToTime(e)

		if common.CompareDate(day, tag) == 0 {
			return e.Text
		}
	}

	return ""
}

func formatDuration(t time.Duration) string {
	if *minutes {
		return fmt.Sprintf("%v", t.Truncate(time.Minute).Minutes())
	} else {
		return fmt.Sprintf("%v", t.Truncate(time.Second))
	}
}

func writeWorktimes(filename string, lines *[]Day) error {
	var fileWorktime *os.File
	var fileExport *os.File
	var err error

	if common.IsRunningAsService() {
		dir := filepath.Dir(filename)

		if !common.FileExists(dir) {
			err := os.MkdirAll(dir, common.DefaultDirMode)

			if common.Error(err) {
				return err
			}
		}

		fileWorktime, err = os.Create(filename)
		if common.Error(err) {
			return err
		}
		defer func() {
			common.DebugError(fileWorktime.Close())
		}()
	}

	if len(*export) > 0 {
		fileExport, err = os.Create(*export)
		if common.Error(err) {
			return err
		}

		_, err := fmt.Fprint(fileExport, "Start/End;Duration day;Duration Week;Comment;Overtime;Sum Overtime\n")
		common.Error(err)
		_, err = fmt.Fprint(fileExport, "\n")
		common.Error(err)

		defer func() {
			common.DebugError(fileExport.Close())
		}()
	}

	var sumWorktime time.Duration
	var sumOvertime time.Duration
	var sumWorkDays int64
	var sumNonWorkDays int64
	var sumOfWeek time.Duration
	var sumOfWeekString string

	commentDay := make(map[string]int)

	start := (*lines)[0].start
	end := time.Now()

	if !limitDate.IsZero() {
		end = limitDate
	}

	completeWeek := false

	start = common.ClearTime(start)
	end = common.ClearTime(end)

	c := 0
	months := 0
	sumVacation := 0

	for loopDay := start; end.Sub(loopDay) >= 0; {

		// d, err := time.Parse(common.DateMask, "25.10.2017")
		// if common.Error(err) {
		// 	return err
		// }

		// if !loopDay.Before(d) {
		// 	fmt.Printf("stop %s\n" + loopDay.Format(common.DateMask))
		// }

		day, found := findDay(lines, loopDay)

		if !found {
			day.start = common.TruncateTime(loopDay, common.Day)
			day.end = common.TruncateTime(loopDay, common.Day)
		}

		if day.start.Day() == 1 {
			months++
		}

		feiertag := getFeiertag(loopDay)

		isFeiertag := len(feiertag) > 0
		isWeekend := loopDay.Weekday() == time.Saturday || loopDay.Weekday() == time.Sunday
		isCommented := len(day.comment) > 0
		isGleittag := day.comment == Gleittag

		if day.start.Day() == 1 && day.start.Month() == 4 {
			sumVacation = 0
		}

		if day.comment == Urlaub {
			sumVacation++
		}

		worktime := time.Duration(0)
		overtime := time.Duration(0)

		if isWeekend || isFeiertag || isGleittag || isCommented {
			day.start = common.TruncateTime(loopDay, common.Day)
			day.end = common.TruncateTime(loopDay, common.Day)

			if isGleittag {
				overtime = -time.Duration(8) * time.Hour
			}

			if isWeekend {
				switch loopDay.Weekday() {
				case time.Saturday:
					day.comment = "#Saturday"
				case time.Sunday:
					day.comment = "#Sunday"
				}
			}

			sumNonWorkDays++
		} else {
			worktime = day.end.Sub(day.start)

			// lunch time
			if worktime > time.Duration(6)*time.Hour {
				worktime -= time.Duration(30) * time.Minute
			}

			// overtime
			if worktime > time.Duration(8)*time.Hour {
				overtime = worktime - time.Duration(8)*time.Hour
				if overtime > time.Duration(2)*time.Hour {
					overtime = time.Duration(2) * time.Hour
				}
			} else {
				overtime += worktime - time.Duration(8)*time.Hour
			}

			sumWorkDays++
		}

		if len(day.comment) == 0 && !isFeiertag && !isWeekend && !isCommented && worktime == 0 {
			day.comment = "?"

			if day.start.Hour() == 0 && day.start.Minute() == 0 && day.end.Hour() == 0 && day.end.Minute() == 0 {
				day.start = time.Date(day.start.Year(), day.start.Month(), day.start.Day(), 8, 0, 0, day.start.Nanosecond(), day.start.Location())
				day.end = time.Date(day.start.Year(), day.start.Month(), day.start.Day(), 17, 0, 0, day.start.Nanosecond(), day.start.Location())

				worktime = time.Duration(8 * time.Hour)
			}
		}

		sumWorktime += worktime
		sumOvertime += overtime

		worktimeString := formatDuration(worktime)
		overtimeString := formatDuration(overtime)

		if day.start.Weekday() == time.Monday {
			completeWeek = true
			sumOfWeek = worktime
		} else {
			sumOfWeek += worktime
		}

		if day.start.Weekday() == time.Friday {
			if !completeWeek {
				sumOfWeekString = formatDuration(time.Duration(40) * time.Hour)
			} else {
				sumOfWeekString = formatDuration(time.Duration(sumOfWeek))
			}
		} else {
			sumOfWeekString = ""
		}

		if strings.HasPrefix(feiertag, "#") {
			s := strconv.Itoa(loopDay.Year()) + " " + feiertag

			v, ok := commentDay[s]
			if ok {
				v++
			} else {
				v = 1
			}

			commentDay[s] = v
		}

		line0 := fmt.Sprintf("%s\n", strings.Join([]string{day.start.Format(string(mask)), "", "", day.comment, ""}, ";"))
		line1 := fmt.Sprintf("%s\n", strings.Join([]string{day.end.Format(string(mask)), worktimeString, sumOfWeekString, "", overtimeString, formatDuration(sumOvertime)}, ";"))

		if common.IsRunningAsService() {
			_, err := fmt.Fprint(fileWorktime, line0)
			common.Error(err)
			_, err = fmt.Fprint(fileWorktime, line1)
			common.Error(err)
		}

		if fileExport != nil {
			_, err := fmt.Fprint(fileExport, line0)
			common.Error(err)
			_, err = fmt.Fprint(fileExport, line1)
			common.Error(err)
		}

		c++
		fmt.Printf("%-3d : %s", c, line0)
		c++
		fmt.Printf("%-3d : %s", c, line1)

		loopDay = loopDay.AddDate(0, 0, 1)
	}

	if !common.IsRunningAsService() {
		averageWorktime := time.Duration(float64(sumWorktime) / float64(sumWorkDays))

		if sumWorkDays < 2 {
			averageWorktime = 0
			sumOvertime = 0
		}

		fmt.Println()

		fmt.Println()
		fmt.Printf("Count worktime days     : %v\n", sumWorkDays)
		fmt.Printf("Count non worktime days : %v\n", sumNonWorkDays)
		fmt.Printf("Average worktime        : %v\n", formatDuration(averageWorktime))
		fmt.Printf("Sum worktime            : %v\n", formatDuration(sumWorktime))
		fmt.Printf("Sum overtime            : %v\n", formatDuration(sumOvertime))
		fmt.Printf("Sum vacation            : %v\n", sumVacation)
		fmt.Printf("Sum vacation left       : %v\n", int(float64(months)**vacationPerMonth)-sumVacation)

		if fileExport != nil {
			_, err := fmt.Fprintf(fileExport, "\n")
			common.Error(err)
			_, err = fmt.Fprintf(fileExport, "Count worktime days     : %v\n", sumWorkDays)
			common.Error(err)
			_, err = fmt.Fprintf(fileExport, "Count non worktime days : %v\n", sumNonWorkDays)
			common.Error(err)
			_, err = fmt.Fprintf(fileExport, "Average worktime        : %v\n", formatDuration(averageWorktime))
			common.Error(err)
			_, err = fmt.Fprintf(fileExport, "Sum worktime            : %v\n", formatDuration(sumWorktime))
			common.Error(err)
			_, err = fmt.Fprintf(fileExport, "Sum overtime            : %v\n", formatDuration(sumOvertime))
			common.Error(err)
		}
	}

	return err
}

func run() error {
	var start time.Time
	var end time.Time
	var lines []Day

	if common.FileExists(*filename) {
		if common.IsRunningAsService() {
			yesterday := time.Now().Add(-time.Hour * 24)

			backupFilename := filepath.Dir(*filename) + string(filepath.Separator) + common.FileNamePart(*filename) + "-" + yesterday.Format(common.SortedDateMask) + common.FileNameExt(*filename)

			if !common.FileExists(backupFilename) {
				err := common.FileCopy(*filename, backupFilename)
				if common.Error(err) {
					return err
				}
			}
		}

		err := readWorktimes(*filename, &start, &end, &lines)
		if common.Error(err) {
			return err
		}
	}

	if start.IsZero() {
		start = time.Now()
	}

	end = time.Now()

	if limitDate.IsZero() {
		if _, found := findDay(&lines, time.Now()); !found {
			lines = append(lines, Day{start, end, ""})
		}
	}

	return common.ExitOrError(writeWorktimes(*filename, &lines))
}

//func debugLines(lines *[]Day) {
//	fmt.Printf("-------- start\n")
//	for _, day := range *lines {
//		fmt.Printf("%s\n", day.start.Format(common.DateTimeMask))
//		fmt.Printf("%s\n", day.end.Format(common.DateTimeMask))
//	}
//	fmt.Printf("-------- end\n")
//}

func main() {
	if common.IsRunningInteractive() {
		common.Run(nil)
	} else {
		common.Run([]string{"f"})
	}
}
