# MeFuzz, the distance based distributed fuzzing.

MeFuzz is written and maintained by

 * [Павел Шлыков](https://t.me/tw02h00ty) <pavel84118@gmail.com>

Also MeFuzz is based on LibAFL library which is written and maintained by

* [Andrea Fioraldi](https://twitter.com/andreafioraldi) <andrea@aflplus.plus>
* [Dominik Maier](https://twitter.com/domenuk) <dominik@aflplus.plus>
* [s1341](https://twitter.com/srubenst1341) <github@shmarya.net>
* [Dongjia Zhang](https://github.com/tokatoka) <toka@aflplus.plus>

## Why MeFuzz?

MeFuzz gives you many of the benefits of an off-the-shelf fuzzer, while being completely customizable.
You can choose from a variety of fuzzers available in LibAFL and easily add your own.
Then, using the distance based analysis, the most successful fuzzer configuration will be automatically selected.
Also MeFuzz collects testcases and their runtime information at one easily monitoring place.
Some highlight features currently include:
- `multi platform`: MeFuzz was confirmed to work on *Windows*, *MacOS*, *Linux*, and *Android* on *x86_64* and *aarch64*.
- `scalability`: MeFuzz was developed to make fuzzing more scalability.
- `addaptive`: Because of the algorithm of distance based fuzzer configuration selection, it is a universal fuzzing tool for different kind of software.

## Getting started
- - - 
#### License

<sup>
Licensed under either of <a href="LICENSE-APACHE">Apache License, Version
2.0</a> or <a href="LICENSE-MIT">MIT license</a> at your option.
</sup>

<br>

<sub>
Unless you explicitly state otherwise, any contribution intentionally submitted
for inclusion in this crate by you, as defined in the Apache-2.0 license, shall
be dual licensed as above, without any additional terms or conditions.
</sub>

<br>

<sub>
Dependencies under more restrictive licenses, such as GPL or AGPL, can be enabled
using the respective feature in each crate when it is present, such as the
'agpl' feature of the libafl crate.
</sub>
