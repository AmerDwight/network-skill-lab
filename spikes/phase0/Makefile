SHELL := /bin/bash
SPIKES := s0 s1 s2 s3 s4

.PHONY: all $(SPIKES) clean

all: $(SPIKES)

$(SPIKES):
	@bash ./$@.sh

clean:
	@docker ps -aq --filter name=nsl- | xargs -r docker rm -f
	@docker network ls -q --filter name=nsl- | xargs -r docker network rm
