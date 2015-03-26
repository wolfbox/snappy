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
	. "launchpad.net/gocheck"
)

func (s *SnapTestSuite) TestRollbackWithVersion(c *C) {
	makeTwoTestSnaps(c, SnapTypeApp)
	c.Assert(ActiveSnapByName("foo").Version(), Equals, "2.0")

	// rollback with version
	version, err := Rollback("foo", "1.0")
	c.Assert(err, IsNil)
	c.Assert(version, Equals, "1.0")

	c.Assert(ActiveSnapByName("foo").Version(), Equals, "1.0")
}

func (s *SnapTestSuite) TestRollbackFindVersion(c *C) {
	makeTwoTestSnaps(c, SnapTypeApp)
	c.Assert(ActiveSnapByName("foo").Version(), Equals, "2.0")

	// rollback without version
	version, err := Rollback("foo", "")
	c.Assert(err, IsNil)
	c.Assert(version, Equals, "1.0")

	c.Assert(ActiveSnapByName("foo").Version(), Equals, "1.0")
}
