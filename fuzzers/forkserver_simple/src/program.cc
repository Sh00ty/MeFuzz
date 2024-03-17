#include <stddef.h>
#include <stdint.h>
#include <string.h>
#include <vector>
#include <unistd.h>
#include "./libpng-1.6.37/png.h"

#ifndef __AFL_FUZZ_TESTCASE_LEN
  ssize_t fuzz_len;
  #define __AFL_FUZZ_TESTCASE_LEN fuzz_len
  unsigned char fuzz_buf[1024000];
  #define __AFL_FUZZ_TESTCASE_BUF fuzz_buf
  #define __AFL_FUZZ_INIT() void sync(void);
  #define __AFL_LOOP(x) ((fuzz_len = read(0, fuzz_buf, sizeof(fuzz_buf))) > 0 ? 1 : 0)
  #define __AFL_INIT() sync()
#endif


// #define PNG_INTERNAL


#define PNG_CLEANUP                                                        \
  if (png_handler.png_ptr) {                                               \
    if (png_handler.row_ptr) {                                             \
      png_free(png_handler.png_ptr, png_handler.row_ptr);                  \
    }                                                                      \
    if (png_handler.end_info_ptr) {                                        \
      png_destroy_read_struct(&png_handler.png_ptr, &png_handler.info_ptr, \
                              &png_handler.end_info_ptr);                  \
    } else if (png_handler.info_ptr) {                                     \
      png_destroy_read_struct(&png_handler.png_ptr, &png_handler.info_ptr, \
                              nullptr);                                    \
    } else {                                                               \
      png_destroy_read_struct(&png_handler.png_ptr, nullptr, nullptr);     \
    }                                                                      \
    png_handler.png_ptr = nullptr;                                         \
    png_handler.row_ptr = nullptr;                                         \
    png_handler.info_ptr = nullptr;                                        \
    png_handler.end_info_ptr = nullptr;                                    \
  }

// The following line is needed for shared memeory testcase fuzzing
__AFL_FUZZ_INIT();

struct BufState {
  const uint8_t *data;
  size_t         bytes_left;
};

struct PngObjectHandler {
  png_infop   info_ptr = nullptr;
  png_structp png_ptr = nullptr;
  png_infop   end_info_ptr = nullptr;
  png_voidp   row_ptr = nullptr;
  BufState   *buf_state = nullptr;

  ~PngObjectHandler() {
    if (row_ptr) { png_free(png_ptr, row_ptr); }
    if (end_info_ptr) {
      png_destroy_read_struct(&png_ptr, &info_ptr, &end_info_ptr);
    } else if (info_ptr) {
      png_destroy_read_struct(&png_ptr, &info_ptr, nullptr);
    } else {
      png_destroy_read_struct(&png_ptr, nullptr, nullptr);
    }
    delete buf_state;
  }
};

void user_read_data(png_structp png_ptr, png_bytep data, size_t length) {
  BufState *buf_state = static_cast<BufState *>(png_get_io_ptr(png_ptr));
  if (length > buf_state->bytes_left) { png_error(png_ptr, "read error"); }
  memcpy(data, buf_state->data, length);
  buf_state->bytes_left -= length;
  buf_state->data += length;
}

static const int kPngHeaderSize = 8;

// Entry point for LibFuzzer.
// Roughly follows the libpng book example:
// http://www.libpng.org/pub/png/book/chapter13.html
int main(int argc, char **argv) {
  FILE *file = stdin;
  if (argc > 1) { file = fopen(argv[1], "rb"); }

 const uint8_t *data = (uint8_t*)__AFL_FUZZ_TESTCASE_BUF;
  while (__AFL_LOOP(1)) {
    auto size = __AFL_FUZZ_TESTCASE_LEN;
    if (size < kPngHeaderSize) { return 0; }

    std::vector<unsigned char> v(data, data + size);
    if (png_sig_cmp(v.data(), 0, kPngHeaderSize)) {
      // not a PNG.
      return 0;
    }

    PngObjectHandler png_handler;
    png_handler.png_ptr = nullptr;
    png_handler.row_ptr = nullptr;
    png_handler.info_ptr = nullptr;
    png_handler.end_info_ptr = nullptr;

    png_handler.png_ptr =
        png_create_read_struct(PNG_LIBPNG_VER_STRING, nullptr, nullptr, nullptr);
    if (!png_handler.png_ptr) { return 0; }

    png_handler.info_ptr = png_create_info_struct(png_handler.png_ptr);
    if (!png_handler.info_ptr) {
      PNG_CLEANUP
      return 0;
    }

    png_handler.end_info_ptr = png_create_info_struct(png_handler.png_ptr);
    if (!png_handler.end_info_ptr) {
      PNG_CLEANUP
      return 0;
    }

    png_set_crc_action(png_handler.png_ptr, PNG_CRC_QUIET_USE, PNG_CRC_QUIET_USE);
  #ifdef PNG_IGNORE_ADLER32
    png_set_option(png_handler.png_ptr, PNG_IGNORE_ADLER32, PNG_OPTION_ON);
  #endif

    // Setting up reading from buffer.
    png_handler.buf_state = new BufState();
    png_handler.buf_state->data = data + kPngHeaderSize;
    png_handler.buf_state->bytes_left = size - kPngHeaderSize;
    png_set_read_fn(png_handler.png_ptr, png_handler.buf_state, user_read_data);
    png_set_sig_bytes(png_handler.png_ptr, kPngHeaderSize);

    if (setjmp(png_jmpbuf(png_handler.png_ptr))) {
      PNG_CLEANUP
      return 0;
    }

    // Reading.
    png_read_info(png_handler.png_ptr, png_handler.info_ptr);

    // reset error handler to put png_deleter into scope.
    if (setjmp(png_jmpbuf(png_handler.png_ptr))) {
      PNG_CLEANUP
      return 0;
    }

    png_uint_32 width, height;
    int         bit_depth, color_type, interlace_type, compression_type;
    int         filter_type;

    if (!png_get_IHDR(png_handler.png_ptr, png_handler.info_ptr, &width, &height,
                      &bit_depth, &color_type, &interlace_type, &compression_type,
                      &filter_type)) {
      PNG_CLEANUP
      return 0;
    }

    // This is going to be too slow.
    if (width && height > 100000000 / width) {
      PNG_CLEANUP
  #ifdef HAS_DUMMY_CRASH
    #ifdef __aarch64__
      asm volatile(".word 0xf7f0a000\n");
    #else
      asm("ud2");
    #endif
  #endif
      return 0;
    }

    // Set several transforms that browsers typically use:
    png_set_gray_to_rgb(png_handler.png_ptr);
    png_set_expand(png_handler.png_ptr);
    png_set_packing(png_handler.png_ptr);
    png_set_scale_16(png_handler.png_ptr);
    png_set_tRNS_to_alpha(png_handler.png_ptr);

    int passes = png_set_interlace_handling(png_handler.png_ptr);

    png_read_update_info(png_handler.png_ptr, png_handler.info_ptr);

    png_handler.row_ptr =
        png_malloc(png_handler.png_ptr,
                  png_get_rowbytes(png_handler.png_ptr, png_handler.info_ptr));

    for (int pass = 0; pass < passes; ++pass) {
      for (png_uint_32 y = 0; y < height; ++y) {
        png_read_row(png_handler.png_ptr,
                    static_cast<png_bytep>(png_handler.row_ptr), nullptr);
      }
    }

    png_read_end(png_handler.png_ptr, png_handler.end_info_ptr);

    PNG_CLEANUP
  }
  return 0;
}
