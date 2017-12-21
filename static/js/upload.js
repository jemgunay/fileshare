$(document).ready(function() {
    // init dropzone
    Dropzone.options.fileInput = {
        paramName: "file-input", // The name that will be used to transfer the file
        maxFilesize: 10, // MB

        init: function() {
            this.on("success", function(file, response) {
                //console.log(response);

                $("#upload-results-panel").append(response);

            });
        }
    };

});