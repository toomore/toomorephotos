package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/toomore/lazyflickrgo/jsonstruct"
)

// genericTitleExpr matches camera-default / placeholder titles that carry no
// curatorial signal (DSC_1234, IMG-0001, P1000123, 純數字, untitled…).
var genericTitleExpr = regexp.MustCompile(`(?i)^\s*((dsc|dscf|dscn|img|imgp|imag|p|pxl|gopr|dji|sam|mvimg|pano|vid)[-_ ]?\d+|\d{2,}|image\s*\d*|photo\s*\d*|untitled.*)\s*$`)

// runAudit samples the DB-stored photo metadata and prints a richness report
// that informs whether AI curation can rely on existing text or needs a
// one-off vision-enrichment pass. Read-only; does not mutate anything.
func runAudit(app *App) error {
	if app.DB == nil {
		log.Fatal("audit 需要 DATABASE_URL，請設定環境變數後再執行")
	}
	ctx := context.Background()
	rows, err := app.DB.Pool().Query(ctx, `SELECT info_json FROM photos`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var (
		total                                     int
		titleEmpty, titleGeneric, titleMeaningful int
		descEmpty, descSubstantial, descLenSum    int
		tag0, tag12, tag35, tag6, tagSum          int
		takenPresent, takenFallback               int
		geoCoord, geoNamed                        int
	)
	tagFreq := map[string]int{}
	countryFreq := map[string]int{}
	var monthHist [13]int // index 1..12

	for rows.Next() {
		var infoJSON []byte
		if err := rows.Scan(&infoJSON); err != nil {
			return err
		}
		var info jsonstruct.PhotosGetInfo
		if err := json.Unmarshal(infoJSON, &info); err != nil {
			continue
		}
		total++
		p := info.Photo

		title := strings.TrimSpace(p.Title.Content)
		switch {
		case title == "":
			titleEmpty++
		case genericTitleExpr.MatchString(title):
			titleGeneric++
		default:
			titleMeaningful++
		}

		desc := strings.TrimSpace(p.Description.Content)
		if desc == "" {
			descEmpty++
		} else {
			n := len([]rune(desc))
			descLenSum += n
			if n >= 20 {
				descSubstantial++
			}
		}

		nt := len(p.Tags.Tag)
		tagSum += nt
		switch {
		case nt == 0:
			tag0++
		case nt <= 2:
			tag12++
		case nt <= 5:
			tag35++
		default:
			tag6++
		}
		for _, t := range p.Tags.Tag {
			if t.Raw != "" {
				tagFreq[t.Raw]++
			}
		}

		if taken := strings.TrimSpace(p.Dates.Taken); taken != "" {
			takenPresent++
			if ts, err := time.Parse("2006-01-02 15:04:05", taken); err == nil {
				monthHist[int(ts.Month())]++
				if up, err := strconv.ParseInt(p.Dateuploaded, 10, 64); err == nil && up > 0 {
					if diff := ts.Unix() - up; diff < 24*3600 && diff > -24*3600 {
						takenFallback++ // taken ~ upload time → likely no real EXIF date
					}
				}
			}
		}

		if p.Location.Latitude != "" && p.Location.Latitude != "0" {
			geoCoord++
		}
		country := strings.TrimSpace(p.Location.Country.Content)
		if country != "" || strings.TrimSpace(p.Location.Locality.Content) != "" {
			geoNamed++
		}
		if country != "" {
			countryFreq[country]++
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	pct := func(n int) string {
		if total == 0 {
			return "0%"
		}
		return fmt.Sprintf("%.0f%%", float64(n)*100/float64(total))
	}

	fmt.Println("================ Metadata 豐富度稽核 ================")
	fmt.Printf("照片總數: %d\n\n", total)
	if total == 0 {
		fmt.Println("DB 內沒有照片，請先執行: ./toomorephotos -sync")
		return nil
	}

	fmt.Println("── 標題 (Title) ──")
	fmt.Printf("  有意義        : %5d (%s)\n", titleMeaningful, pct(titleMeaningful))
	fmt.Printf("  通用/相機檔名 : %5d (%s)\n", titleGeneric, pct(titleGeneric))
	fmt.Printf("  空白          : %5d (%s)\n\n", titleEmpty, pct(titleEmpty))

	avgDesc := 0
	if total-descEmpty > 0 {
		avgDesc = descLenSum / (total - descEmpty)
	}
	fmt.Println("── 描述 (Description) ──")
	fmt.Printf("  有實質內容(≥20字): %5d (%s)\n", descSubstantial, pct(descSubstantial))
	fmt.Printf("  空白             : %5d (%s)\n", descEmpty, pct(descEmpty))
	fmt.Printf("  非空白者平均長度 : %d 字\n\n", avgDesc)

	fmt.Println("── 標籤 (Tags) ──")
	fmt.Printf("  平均每張: %.1f 個\n", float64(tagSum)/float64(total))
	fmt.Printf("  0個:%d(%s)  1-2個:%d(%s)  3-5個:%d(%s)  6+個:%d(%s)\n",
		tag0, pct(tag0), tag12, pct(tag12), tag35, pct(tag35), tag6, pct(tag6))
	printTop("  最常見標籤", tagFreq, 15)
	fmt.Println()

	fmt.Println("── 拍攝日期 (季節主題可行性) ──")
	fmt.Printf("  有 taken 日期        : %5d (%s)\n", takenPresent, pct(takenPresent))
	fmt.Printf("  疑似上傳時間 fallback: %5d (%s)  ← 此比例的季節判斷不可信\n", takenFallback, pct(takenFallback))
	fmt.Print("  月份分布: ")
	for m := 1; m <= 12; m++ {
		fmt.Printf("%d月=%d ", m, monthHist[m])
	}
	fmt.Print("\n\n")

	fmt.Println("── 地點 (地點主題可行性) ──")
	fmt.Printf("  有經緯度         : %5d (%s)\n", geoCoord, pct(geoCoord))
	fmt.Printf("  有地名(含 locality/country): %5d (%s)  ← 有地名就免反向地理編碼\n", geoNamed, pct(geoNamed))
	printTop("  最常見國家", countryFreq, 10)
	fmt.Println()

	mtp := float64(titleMeaningful) / float64(total)
	sdp := float64(descSubstantial) / float64(total)
	avgTags := float64(tagSum) / float64(total)
	fmt.Println("── 自動判斷 (啟發式，僅供參考) ──")
	switch {
	case mtp < 0.5 && sdp < 0.3 && avgTags < 3:
		fmt.Println("  文字訊號明顯偏弱 → 強烈建議做一次視覺 enrichment，否則純文字策展會盲選。")
	case mtp < 0.6 || sdp < 0.4 || avgTags < 4:
		fmt.Println("  文字訊號中等 → 季節/tag 主題可行；但「以影像/色彩策展」建議補視覺 enrichment。")
	default:
		fmt.Println("  文字訊號充足 → 可先嘗試純文字策展，視覺 enrichment 之後再視成效決定。")
	}
	return nil
}

// printTop prints the top-n entries of a frequency map, descending by count.
func printTop(label string, freq map[string]int, n int) {
	type kv struct {
		k string
		v int
	}
	pairs := make([]kv, 0, len(freq))
	for k, v := range freq {
		pairs = append(pairs, kv{k, v})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].v != pairs[j].v {
			return pairs[i].v > pairs[j].v
		}
		return pairs[i].k < pairs[j].k
	})
	if len(pairs) > n {
		pairs = pairs[:n]
	}
	parts := make([]string, len(pairs))
	for i, p := range pairs {
		parts[i] = fmt.Sprintf("%s(%d)", p.k, p.v)
	}
	fmt.Printf("%s: %s\n", label, strings.Join(parts, " "))
}
