TEST_FLAGS =
DEV_TAGS = dev
COVER_PKG = $$(go list -deps ./... | grep '$(PKG)')

# Add the build tag for running unit tests against a postgres DB.
ifeq ($(dbbackend),postgres)
DEV_TAGS += test_db_postgres
else
DEV_TAGS += test_db_sqlite
endif

# Add any additional tags that are passed in to make.
ifneq ($(tags),)
DEV_TAGS += ${tags}
endif

# If specific package is being unit tested, construct the full name of the
# subpackage.
ifneq ($(pkg),)
UNITPKG := $(PKG)/$(pkg)
UNIT_TARGETED = yes
COVER_PKG = $(PKG)/$(pkg)
endif

# If a specific unit test case is being target, construct test.run filter.
ifneq ($(case),)
TEST_FLAGS += -test.run=$(case)
UNIT_TARGETED = yes
endif

# If a timeout was requested, construct initialize the proper flag for the go
# test command. If not, we set 20m (up from the default 10m).
ifneq ($(timeout),)
TEST_FLAGS += -test.timeout=$(timeout)
else
TEST_FLAGS += -test.timeout=20m
endif

# UNIT_TARGTED is undefined iff a specific package and/or unit test case is
# not being targeted.
UNIT_TARGETED ?= no

# If a specific package/test case was requested, run the unit test for the
# targeted case. Otherwise, default to running all tests.
ifeq ($(UNIT_TARGETED), yes)
UNIT := $(GOTEST) -tags="$(DEV_TAGS)" $(TEST_FLAGS) $(UNITPKG)
UNIT_RACE := $(GOTEST) -tags="$(DEV_TAGS)" $(TEST_FLAGS) -race $(UNITPKG)
endif

ifeq ($(UNIT_TARGETED), no)
UNIT := $(GOLIST) | $(XARGS) env $(GOTEST) -tags="$(DEV_TAGS)" $(TEST_FLAGS)
UNIT_RACE := $(UNIT) -race
endif
