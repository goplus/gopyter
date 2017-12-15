/*
 * gomacro - A Go interpreter with Lisp-like macros
 *
 * Copyright (C) 2017 Massimiliano Ghilardi
 *
 *     This program is free software: you can redistribute it and/or modify
 *     it under the terms of the GNU Lesser General Public License as published
 *     by the Free Software Foundation, either version 3 of the License, or
 *     (at your option) any later version.
 *
 *     This program is distributed in the hope that it will be useful,
 *     but WITHOUT ANY WARRANTY; without even the implied warranty of
 *     MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 *     GNU Lesser General Public License for more details.
 *
 *     You should have received a copy of the GNU Lesser General Public License
 *     along with this program.  If not, see <https://www.gnu.org/licenses/lgpl>.
 *
 *
 * read.go
 *
 *  Created on: Mar 12, 2017
 *      Author: Massimiliano Ghilardi
 */

package base

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"go/token"
	"io"
	r "reflect"

	mt "github.com/cosmos72/gomacro/token"
)

func ReadBytes(src interface{}) []byte {
	switch s := src.(type) {
	case []byte:
		if s != nil {
			return s
		}
	case string:
		return []byte(s)
	case *bytes.Buffer:
		// is io.Reader, but src is already available in []byte form
		if s != nil {
			return s.Bytes()
		}
	case io.Reader:
		if s != nil {
			var buf bytes.Buffer
			if _, err := io.Copy(&buf, s); err != nil {
				Error(err)
			}
			return buf.Bytes()
		}
	}
	Errorf("unsupported source, cannot read from: %v <%v>", src, r.TypeOf(src))
	return nil
}

func ReadString(src interface{}) string {
	switch s := src.(type) {
	case []byte:
		if s != nil {
			return string(s)
		}
	case string:
		return s
	case *bytes.Buffer:
		// is io.Reader, but src is already available in string form
		if s != nil {
			return s.String()
		}
	case io.Reader:
		if s != nil {
			var buf bytes.Buffer
			if _, err := io.Copy(&buf, s); err != nil {
				Error(err)
			}
			return buf.String()
		}
	}
	Errorf("unsupported source, cannot read from: %v <%v>", src, r.TypeOf(src))
	return ""
}

type ReadOptions int

const (
	ReadOptShowPrompt         ReadOptions = 1 << iota
	ReadOptCollectAllComments             // continue until non-comment is found. default is to return comments one by one
)

const debug = false

type mode int

const (
	mNormal mode = iota
	mPlus
	mMinus
	mRune
	mString
	mRuneEscape
	mStringEscape
	mRawString
	mSlash
	mHash
	mLineComment
	mComment
	mCommentStar
	mTilde
)

func (m mode) String() string {
	switch m {
	case mNormal:
		return "norm"
	case mPlus:
		return "plus"
	case mMinus:
		return "minus"
	case mRune:
		return "rune"
	case mString:
		return "string"
	case mRuneEscape:
		return "runesc"
	case mStringEscape:
		return "stresc"
	case mRawString:
		return "strraw"
	case mSlash:
		return "slash"
	case mHash:
		return "hash"
	case mLineComment:
		return "lcomm"
	case mComment:
		return "comment"
	case mCommentStar:
		return "comm*"
	case mTilde:
		return "tilds"
	default:
		return "???"
	}
}

var paragraph_separator_bytes = []byte{0xe2, 0x80, 0xa9}
var nl_bytes = []byte{'\n'}

func ReadMultiline(in *bufio.Reader, opts ReadOptions, out io.Writer, prompt string) (src string, firstToken int, err error) {
	m := mNormal
	paren := 0
	optPrompt := opts&ReadOptShowPrompt != 0
	optAllComments := opts&ReadOptCollectAllComments != 0
	ignorenl := false
	firstToken = -1
	lastToken := -1

	if optPrompt {
		fmt.Fprint(out, prompt)
	}
	var line, buf []byte

	// comments do not reset ignorenl
	resetnl := func(paren int, m mode) bool {
		return paren != 0 ||
			(m != mNormal && m != mSlash && m != mHash &&
				m != mLineComment && m != mComment && m != mCommentStar)
	}
	foundtoken := func(pos int) {
		lastToken = len(buf) + pos
		if firstToken < 0 {
			firstToken = lastToken
			if debug {
				Debugf("ReadMultiline: setting firstToken to %d, line up to it = %q", firstToken, line[:pos])
			}
		}
	}
	invalidChar := func(i int, ch byte, ctx string) (string, int, error) {
		return string(append(buf, line[:i]...)), firstToken,
			errors.New(fmt.Sprintf("unexpected character %q inside %s literal", ch, ctx))
	}

	for {
		line, err = in.ReadBytes('\n')
		line = bytes.Replace(line, paragraph_separator_bytes, nl_bytes, -1)
		for i, ch := range line {
			if debug {
				Debugf("ReadMultiline: found %q\tmode=%v\tparen=%d ignorenl=%t", ch, m, paren, ignorenl)
			}
			switch m {
			case mPlus, mMinus:
				if ch == '+' {
					if m == mPlus {
						m = mNormal
					} else {
						m = mPlus
					}
					break
				} else if ch == '-' {
					if m == mMinus {
						m = mNormal
					} else {
						m = mMinus
					}
					break
				}
				m = mNormal
				ignorenl = true
				if ch <= ' ' {
					continue
				}
				fallthrough
			case mNormal:
				switch ch {
				case '(', '[', '{':
					paren++
				case ')', ']', '}':
					paren--
				case '\'':
					m = mRune
				case '"':
					m = mString
				case '`':
					m = mRawString
				case '/':
					m = mSlash
					continue // no tokens yet
				case '#':
					m = mHash // support #! line comments
					continue  // no tokens yet
				case '~':
					m = mTilde
				case '!', '%', '&', '*', ',', '.', '<', '=', '>', '^', '|':
					ignorenl = paren == 0
				case '+':
					ignorenl = false
					if paren == 0 {
						m = mPlus
					}
				case '-':
					ignorenl = false
					if paren == 0 {
						m = mMinus
					}
				default:
					if ch <= ' ' {
						continue // not a token
					}
					ignorenl = false // found a token
				}
			case mRune:
				switch ch {
				case '\\':
					m = mRuneEscape
				case '\'':
					m = mNormal
				default:
					if ch < ' ' {
						return invalidChar(i, ch, "rune")
					}
				}
			case mRuneEscape:
				if ch < ' ' {
					return invalidChar(i, ch, "rune")
				}
				m = mRune
			case mString:
				switch ch {
				case '\\':
					m = mStringEscape
				case '"':
					m = mNormal
				default:
					if ch < ' ' {
						return invalidChar(i, ch, "string")
					}
				}
			case mStringEscape:
				if ch < ' ' {
					return invalidChar(i, ch, "string")
				}
				m = mString
			case mRawString:
				switch ch {
				case '`':
					m = mNormal
				}
			case mSlash:
				switch ch {
				case '/':
					m = mLineComment
					continue // no tokens
				case '*':
					m = mComment
					continue // no tokens
				default:
					m = mNormal
					if ch <= ' ' {
						ignorenl = true
					} else {
						foundtoken(i - 1)
					}
				}
			case mHash:
				switch ch {
				case '!':
					m = mLineComment
					line[i-1] = '/'
					line[i] = '/'
					continue // no tokens
				default:
					m = mNormal
					foundtoken(i - 1)
				}
			case mLineComment:
				continue
			case mComment:
				switch ch {
				case '*':
					m = mCommentStar
				}
				continue
			case mCommentStar:
				switch ch {
				case '/':
					m = mNormal
				default:
					m = mComment
				}
				continue
			case mTilde:
				m = mNormal
			}
			if debug {
				Debugf("ReadMultiline:          \tmode=%v\tparen=%d ignorenl=%t resetnl=%t", m, paren, ignorenl, resetnl(paren, m))
			}
			if resetnl(paren, m) {
				ignorenl = false
				if debug {
					Debugf("ReadMultiline: cleared ignorenl")
				}
			}
			if ch > ' ' {
				foundtoken(i)
			}
		}
		buf = append(buf, line...)
		if m == mLineComment {
			m = mNormal
		}
		if err != nil {
			break
		}
		if paren <= 0 && !ignorenl && m == mNormal && (firstToken >= 0 || !optAllComments) {
			if firstToken >= 0 && lastIsKeywordIgnoresNl(line, firstToken, lastToken) {
				ignorenl = true
			} else {
				break
			}
		}
		if debug {
			Debugf("ReadMultiline: continuing\tmode=%v\tparen=%d ignorenl=%t", m, paren, ignorenl)
		}
		if m == mPlus || m == mMinus {
			m = mNormal
		}
		if optPrompt {
			printDots(out, 4+2*paren)
		}
	}
	if err != nil {
		if err == io.EOF && paren > 0 {
			err = errors.New("unexpected EOF")
		}
		return string(buf), firstToken, err
	}
	if debug {
		Debugf("ReadMultiline: read %d bytes, firstToken at %d", len(buf), firstToken)
		if firstToken >= 0 {
			Debugf("ReadMultiline: comments: %q", buf[:firstToken])
			Debugf("ReadMultiline: tokens: %q", buf[firstToken:])
		} else {
			Debugf("ReadMultiline: comments: %q", buf)
		}
	}
	return string(buf), firstToken, nil
}

func lastIsKeywordIgnoresNl(line []byte, first, last int) bool {
	if last >= 0 && last < len(line) {
		line = line[:last+1]
	}
	if first >= 0 && first <= len(line) {
		line = line[first:]
	}
	n := len(line)
	var start, end int
	for i := n - 1; i >= 0; i-- {
		ch := line[i]
		if ch <= ' ' {
			continue
		} else if ch >= 'a' && ch <= 'z' {
			end = i + 1
			break
		}
		return false
	}
	for i := end - 1; i >= 0; i-- {
		ch := line[i]
		if ch < 'a' || ch > 'z' {
			start = i + 1
			break
		}
	}
	str := string(line[start:end])
	tok := mt.Lookup(str)
	ignorenl := false
	switch tok {
	case token.IDENT, token.BREAK, token.CONTINUE, token.FALLTHROUGH, token.RETURN:
	default:
		ignorenl = true
	}
	if debug {
		Debugf("lastIsKeywordIgnoresNl: found %ignorenl=%t", str, ignorenl)
	}
	return ignorenl
}

func printDots(out io.Writer, count int) {
	const (
		dots  = ". . . . . . . . . . . . . . . . "
		ndots = len(dots)
	)
	for count >= ndots {
		fmt.Fprint(out, dots)
		count -= ndots
	}
	if count > 0 {
		fmt.Fprint(out, dots[0:count])
	}
}