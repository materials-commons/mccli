class TqdmFile:
    def __init__(self, fp, bar):
        self.fp = fp
        self.bar = bar
    def read(self, n=-1):
        b = self.fp.read(n)
        if b:
            self.bar.update(len(b))
        return b
    def __getattr__(self, name):
        return getattr(self.fp, name)