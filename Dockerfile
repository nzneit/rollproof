# The tool's own image: the static binary as /rollproof and nothing else, the layout of the probe's
# image (design, section 9). It is built from a folder that holds this file and the binary,
# rollproof: ENGINE build -t IMAGE FOLDER.
FROM scratch
COPY rollproof /rollproof
USER 65532:65532
ENTRYPOINT ["/rollproof"]
