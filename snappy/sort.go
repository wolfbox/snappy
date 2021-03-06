/*
 * Copyright (C) 2014-2015 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package snappy

import (
	"log"
	"regexp"
	"strconv"
	"strings"
)

const (
	reDigit           = "[0-9]"
	reAlpha           = "[a-zA-Z]"
	reDigitOrNonDigit = "[0-9]+|[^0-9]+"

	reHasEpoch = "^[0-9]+:"
)

// golang: seriously? that's sad!
func max(a, b int) int {
	if a < b {
		return b
	}
	return a
}

// version number compare, inspired by the libapt/python-debian code
func cmpInt(intA, intB int) int {
	if intA < intB {
		return -1
	} else if intA > intB {
		return 1
	}
	return 0
}

func chOrder(ch uint8) int {
	if ch == '~' {
		return -1
	}
	if matched, _ := regexp.MatchString(reAlpha, string(ch)); matched {
		return int(ch)
	}

	// can only happen if cmpString sets '0' because there is no fragment
	if matched, _ := regexp.MatchString(reDigit, string(ch)); matched {
		return 0
	}

	return int(ch) + 256
}

func cmpString(as, bs string) int {
	for i := 0; i < max(len(as), len(bs)); i++ {
		a := uint8('0')
		b := uint8('0')
		if i < len(as) {
			a = as[i]
		}
		if i < len(bs) {
			b = bs[i]
		}
		if chOrder(a) < chOrder(b) {
			return -1
		}
		if chOrder(a) > chOrder(b) {
			return +1
		}
	}
	return 0
}

func cmpFragment(a, b string) int {
	intA, errA := strconv.Atoi(a)
	intB, errB := strconv.Atoi(b)
	if errA == nil && errB == nil {
		return cmpInt(intA, intB)
	}
	res := cmpString(a, b)
	//fmt.Println(a, b, res)
	return res
}

func getFragments(a string) []string {
	re := regexp.MustCompile(reDigitOrNonDigit)
	matches := re.FindAllString(a, -1)
	return matches
}

// VersionIsValid returns true if the given string is a valid snap
// version number
func VersionIsValid(a string) bool {
	if matched, _ := regexp.MatchString(reHasEpoch, a); matched {
		return false
	}
	if strings.Count(a, "-") > 1 {
		return false
	}
	if strings.TrimSpace(a) == "" {
		return false
	}
	return true
}

func compareSubversion(va, vb string) int {
	fragsA := getFragments(va)
	fragsB := getFragments(vb)

	for i := 0; i < max(len(fragsA), len(fragsB)); i++ {
		a := "0"
		b := "0"
		if i < len(fragsA) {
			a = fragsA[i]
		}
		if i < len(fragsB) {
			b = fragsB[i]
		}
		res := cmpFragment(a, b)
		//fmt.Println(a, b, res)
		if res != 0 {
			return res
		}
	}
	return 0
}

// VersionCompare compare two version strings and
// Returns:
//   -1 if a is smaller than b
//    0 if a equals b
//   +1 if a is bigger than b
func VersionCompare(va, vb string) (res int) {
	if !VersionIsValid(va) {
		log.Printf("Invalid version '%s', using '0' instead. Expect wrong results", va)
		va = "0"
	}
	if !VersionIsValid(vb) {
		log.Printf("Invalid version '%s', using '0' instead. Expect wrong results", vb)
		vb = "0"
	}

	if !strings.Contains(va, "-") {
		va += "-0"
	}
	if !strings.Contains(vb, "-") {
		vb += "-0"
	}

	// the main version number (before the "-")
	mainA := strings.Split(va, "-")[0]
	mainB := strings.Split(vb, "-")[0]
	res = compareSubversion(mainA, mainB)
	if res != 0 {
		return res
	}

	// the subversion revision behind the "-"
	revA := strings.Split(va, "-")[1]
	revB := strings.Split(vb, "-")[1]
	return compareSubversion(revA, revB)
}

// ByVersion provides a sort interface
type ByVersion []string

func (bv ByVersion) Less(a, b int) bool {
	return (VersionCompare(bv[a], bv[b]) < 0)
}
func (bv ByVersion) Swap(a, b int) {
	bv[a], bv[b] = bv[b], bv[a]
}
func (bv ByVersion) Len() int {
	return len(bv)
}

// BySnapVersion provides a sort interface
type BySnapVersion []Part

func (bv BySnapVersion) Less(a, b int) bool {
	return (VersionCompare(bv[a].Version(), bv[b].Version()) < 0)
}
func (bv BySnapVersion) Swap(a, b int) {
	bv[a], bv[b] = bv[b], bv[a]
}
func (bv BySnapVersion) Len() int {
	return len(bv)
}
