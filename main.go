package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kardianos/service"
	"github.com/mpetavy/common"
	"github.com/wlbr/feiertage"
)

const mask = common.Day + common.DateSeparator + common.Month + common.DateSeparator + common.Year + common.Separator + common.Hour + common.TimeSeparator + common.Minute

const (
	Gleittag = "#Gleittag"
)

var (
	filename *string
	minutes  *bool
	export   *string

	listFeiertage []feiertage.Feiertag
	yearFeiertage int
)

type Day struct {
	start   time.Time
	end     time.Time
	comment string
}

func init() {
	common.Init("worktime", "1.0.27", "2017", "tracks your working times", "mpetavy", common.APACHE, "https://github.com/mpetavy/worktime", true, nil, nil, tick, time.Duration(60)*time.Second)

	minutes = flag.Bool("m", false, "show durations in minutes")

	fn := ""

	user, err := user.Current()
	if err != nil {
		panic(err)
	}

	if service.Interactive() {
		fn = fmt.Sprintf("%s%s%s", fmt.Sprintf("%s%s%s%s%s", user.HomeDir, string(os.PathSeparator), "Documents", string(os.PathSeparator), "worktime"), string(os.PathSeparator), "worktime.csv")
	}

	filename = flag.String("f", fn, "filename for worktime.csv")
	export = flag.String("e", "", "filename for export worktime.csv")
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
	if err != nil {
		return err
	}

	defer file.Close()

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
		if err != nil {
			return err
		}

		txt := record[0]
		p := strings.Index(txt, ",")

		if p != -1 {
			txt = txt[0:p]
		}

		date, err := common.ParseDateTime(mask, txt)
		if err != nil {
			return err
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

	if !service.Interactive() {
		dir := filepath.Dir(filename)

		b, err := common.FileExists(dir)
		if !b {
			err := os.MkdirAll(dir, os.ModePerm)

			if err != nil {
				return err
			}
		}

		fileWorktime, err = os.Create(filename)
		if err != nil {
			return err
		}
		defer fileWorktime.Close()
	}

	if len(*export) > 0 {
		fileExport, err = os.Create(*export)
		if err != nil {
			return err
		}

		fmt.Fprint(fileExport, "Start/End;Duration day;Duration Week;Comment;Overtime;Sum Overtime\n")
		fmt.Fprint(fileExport, "\n")

		defer fileExport.Close()
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
	completeWeek := false

	start = common.ClearTime(start)
	end = common.ClearTime(end)

	c := 0

	for loopDay := start; end.Sub(loopDay) >= 0; {

		// d, err := time.Parse(common.DateMask, "25.10.2017")
		// if err != nil {
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

		comment := getFeiertag(loopDay)

		isFeiertag := len(comment) > 0
		isWeekend := loopDay.Weekday() == time.Saturday || loopDay.Weekday() == time.Sunday
		isCommented := len(day.comment) > 0
		isGleittag := isCommented && day.comment == Gleittag

		worktime := time.Duration(0)
		overtime := time.Duration(0)

		if isWeekend || isFeiertag || isGleittag || isCommented {
			day.start = common.TruncateTime(loopDay, common.Day)
			day.end = common.TruncateTime(loopDay, common.Day)

			if isGleittag {
				overtime = -time.Duration(8) * time.Hour
			}

			sumNonWorkDays++
		} else {
			worktime = day.end.Sub(day.start)
			if worktime > time.Duration(6)*time.Hour {
				worktime -= time.Duration(30) * time.Minute
			}

			overtime = worktime - time.Duration(8)*time.Hour

			sumWorkDays++
		}

		sumWorktime += worktime
		sumOvertime += overtime

		overtimeString := formatDuration(overtime)
		worktimeString := formatDuration(worktime)

		if len(comment) == 0 && isCommented {
			comment = day.comment
		}

		if len(comment) == 0 && !isFeiertag && !isWeekend && !isCommented && worktime == 0 {
			comment = "?"
		}

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

		if strings.HasPrefix(comment, "#") {
			s := strconv.Itoa(loopDay.Year()) + " " + comment

			v, ok := commentDay[s]
			if ok {
				v++
			} else {
				v = 1
			}

			commentDay[s] = v
		}

		line0 := fmt.Sprintf("%s\n", strings.Join([]string{day.start.Format(string(mask)), "", "", comment, ""}, ";"))
		line1 := fmt.Sprintf("%s\n", strings.Join([]string{day.end.Format(string(mask)), worktimeString, sumOfWeekString, "", overtimeString, formatDuration(sumOvertime)}, ";"))

		if !service.Interactive() {
			fmt.Fprint(fileWorktime, line0)
			fmt.Fprint(fileWorktime, line1)
		}

		if fileExport != nil {
			fmt.Fprint(fileExport, line0)
			fmt.Fprint(fileExport, line1)
		}

		c++
		fmt.Printf("%-3d : %s", c, line0)
		c++
		fmt.Printf("%-3d : %s", c, line1)

		loopDay = loopDay.AddDate(0, 0, 1)
	}

	if service.Interactive() {
		averageWorktime := time.Duration(float64(sumWorktime) / float64(sumWorkDays))

		if sumWorkDays < 2 {
			averageWorktime = 0
			sumOvertime = 0
		}

		fmt.Println()

		for _, k := range sorted(commentDay) {
			fmt.Printf("%-23s : %v\n", k, commentDay[k])
		}

		fmt.Println()
		fmt.Printf("Count worktime days     : %v\n", sumWorkDays)
		fmt.Printf("Count non worktime days : %v\n", sumNonWorkDays)
		fmt.Printf("Average worktime        : %v\n", formatDuration(averageWorktime))
		fmt.Printf("Sum worktime            : %v\n", formatDuration(sumWorktime))
		fmt.Printf("Sum overtime            : %v\n", formatDuration(sumOvertime))
	}

	return err
}

func sorted(m map[string]int) []string {
	var s []string

	for k := range m {
		s = append(s, k)
	}

	sort.Strings(s)

	return s
}

func tick() error {
	var start time.Time
	var end time.Time
	var lines []Day

	fmt.Printf("worktime file: %s\n\n", *filename)

	b, err := common.FileExists(*filename)
	if err != nil {
		return err
	}

	if b {
		yesterday := time.Now().Add(-time.Hour * 24)

		backupFilename := filepath.Dir(*filename) + string(filepath.Separator) + common.FileNamePart(*filename) + "-" + yesterday.Format(common.DateMaskFilename) + common.FileNameExt(*filename)

		b, err := common.FileExists(backupFilename)
		if err != nil {
			return err
		}
		if !b {
			err := common.FileCopy(*filename, backupFilename)
			if err != nil {
				return err
			}
		}

		err = readWorktimes(*filename, &start, &end, &lines)
		if err != nil {
			return err
		}
	}

	if start.IsZero() {
		start = time.Now()
	}

	end = time.Now()

	if _, found := findDay(&lines, time.Now()); !found {
		lines = append(lines, Day{start, end, ""})
	}

	return writeWorktimes(*filename, &lines)
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
	defer common.Done()

	common.Run([]string{"f"})
}
