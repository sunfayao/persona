package assistant

// Regression for #7619: an open supervised todo in a leader conversation must not shorten an interval
// follow-up. The task runs on its configured `every`, the views show that schedule, and the run prompt
// states the configured interval. The former leader cadence (#3811, #3870, #4014) fired such tasks every
// 10 minutes while a supervised todo was open.
