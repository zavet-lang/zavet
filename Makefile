.PHONY: build test test-examples clean run-examples

# Сборка интерпретатора
build:
	go build -ldflags "-X main.Version=1.1.0" -o zavet .

# Все тесты: golden + examples + удалённые формы
test:
	go test ./tests -v

# Прогнать все примеры через собранный интерпретатор
run-examples: build
	@for f in examples/*.молитва examples/*.ya; do \
		[ -f "$$f" ] || continue; \
		echo "=== $$f ==="; \
		./zavet "$$f"; \
	done

# Уборка
clean:
	rm -f zavet zavet.exe
