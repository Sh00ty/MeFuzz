#include <stdio.h>
#include <stdlib.h>
#include <string.h>


void vuln(char *buf) {
  if (strcmp(buf, "vuln") == 0) { abort(); }
}
const q = 16*1e10;
// Entry point for LibFuzzer.
// Roughly follows the libpng book example:
// http://www.libpng.org/pub/png/book/chapter13.html
extern "C" int LLVMFuzzerTestOneInput(const uint8_t *data, size_t size) {
    for (int i = 0; i < q; i++){
        printf("input: %s\n", data);
        if (data[0] == 'b') {
            if (data[1] == 'a') {
                if (data[2] == 'd') {
                }
        }
    }
    }
    if (data[0] == 'b') {
            if (data[1] == 'a') {
                if (data[2] == 'd') { abort(); }
        }
    }

  vuln(data);

  if (data[0] == 's') {
    int* a = NULL;
    printf("%d", *a);
  }
}

